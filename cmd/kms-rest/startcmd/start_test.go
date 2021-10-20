/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	dctest "github.com/ory/dockertest/v3"
	dc "github.com/ory/dockertest/v3/docker"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/trustbloc/edge-core/pkg/log"
	"github.com/trustbloc/edge-core/pkg/log/mocklogger"
)

const (
	logLevelCritical = "critical"
	logLevelError    = "error"
	logLevelWarn     = "warning"
	logLevelInfo     = "info"
	logLevelDebug    = "debug"
)

type mockServer struct{}

func (s *mockServer) ListenAndServe(host, certFile, keyFile string, router http.Handler) error {
	return nil
}

func (s *mockServer) Logger() log.Logger {
	return &mocklogger.MockLogger{}
}

func TestListenAndServe(t *testing.T) {
	t.Run("test wrong host", func(t *testing.T) {
		var w httpServer
		err := w.ListenAndServe("wronghost", "", "", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "address wronghost: missing port in address")
	})

	t.Run("test invalid key file", func(t *testing.T) {
		var w httpServer
		err := w.ListenAndServe("localhost:8080", "test.key", "test.cert", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "open test.key: no such file or directory")
	})
}

func TestStartCmdContents(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	require.Equal(t, "start", startCmd.Use)
	require.Equal(t, "Start kms-rest", startCmd.Short)
	require.Equal(t, "Start kms-rest inside the kms", startCmd.Long)

	checkFlagPropertiesCorrect(t, startCmd, hostURLFlagName, "", hostURLFlagUsage)
}

func TestStartCmdWithBlankArg(t *testing.T) {
	flags := []string{
		hostURLFlagName, baseURLFlagName, logLevelFlagName,
		tlsServeCertPathFlagName, tlsServeKeyPathFlagName, secretLockKeyPathFlagName,
		databaseTypeFlagName, databaseURLFlagName, databasePrefixFlagName,
		userKeysStorageTypeFlagName, userKeysStorageURLFlagName, userKeysStoragePrefixFlagName,
	}

	t.Parallel()

	for _, f := range flags {
		flag := f
		t.Run(fmt.Sprintf("test blank %s arg", flag), func(t *testing.T) {
			startCmd := GetStartCmd(&mockServer{})

			args := buildAllArgsWithOneBlank(flags, flag)
			startCmd.SetArgs(args)

			err := startCmd.Execute()
			require.Error(t, err)
			require.EqualError(t, err, fmt.Sprintf("%s value is empty", flag))
		})
	}
}

func TestStartCmdWithMissingArg(t *testing.T) {
	t.Run("test missing host-url arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := []string{
			"--" + databaseTypeFlagName, storageTypeMemOption,
			"--" + userKeysStorageTypeFlagName, storageTypeMemOption,
		}
		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Equal(t, "Neither host-url (command line flag) nor "+
			"KMS_HOST_URL (environment variable) have been set.",
			err.Error())
	})

	t.Run("test missing database-type arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := []string{
			"--" + hostURLFlagName, "hostname",
			"--" + userKeysStorageTypeFlagName, storageTypeMemOption,
		}
		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "Neither database-type (command line flag) nor "+
			"KMS_DATABASE_TYPE (environment variable) have been set.",
			err.Error())
	})

	t.Run("test missing user-keys-storage-type arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := []string{
			"--" + hostURLFlagName, "hostname",
			"--" + databaseTypeFlagName, storageTypeMemOption,
		}
		startCmd.SetArgs(args)

		err := startCmd.Execute()

		require.Error(t, err)
		require.Equal(t, "Neither user-keys-storage-type (command line flag) nor "+
			"KMS_USER_KEYS_STORAGE_TYPE (environment variable) have been set.",
			err.Error())
	})
}

func TestStartCmdWithBlankEnvVar(t *testing.T) {
	t.Run("test blank host env var", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		err := os.Setenv(hostURLEnvKey, "")
		require.NoError(t, err)

		err = startCmd.Execute()
		require.Error(t, err)
		require.Equal(t, "KMS_HOST_URL value is empty", err.Error())
	})
}

func TestStartCmdValidArgs(t *testing.T) {
	t.Run("using in-memory storage option", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})
		startCmd.SetArgs(requiredArgs(storageTypeMemOption))

		err := startCmd.Execute()
		require.Nil(t, err)
	})
	t.Run("using MongoDB storage option", func(t *testing.T) {
		pool, mongoDBResource := startMongoDBContainer(t)

		defer func() {
			require.NoError(t, pool.Purge(mongoDBResource), "failed to purge MongoDB resource")
		}()

		startCmd := GetStartCmd(&mockServer{})
		startCmd.SetArgs(requiredArgs(storageTypeMongoDBOption))

		err := startCmd.Execute()
		require.Nil(t, err)
	})
}

func startMongoDBContainer(t *testing.T) (*dctest.Pool, *dctest.Resource) {
	t.Helper()

	pool, err := dctest.NewPool("")
	require.NoError(t, err)

	mongoDBResource, err := pool.RunWithOptions(&dctest.RunOptions{
		Repository: "mongo",
		Tag:        "4.0.0",
		PortBindings: map[dc.Port][]dc.PortBinding{
			"27017/tcp": {{HostIP: "", HostPort: "27017"}},
		},
	})
	require.NoError(t, err)

	return pool, mongoDBResource
}

func TestStartCmdValidArgsEnvVar(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	setEnvVars(t)
	defer unsetEnvVars(t)

	err := startCmd.Execute()
	require.NoError(t, err)
}

func TestStartCmdLogLevels(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		out  log.Level
	}{
		{`Log level not specified - defaults to "info"`, "", log.INFO},
		{"Log level: critical", logLevelCritical, log.CRITICAL},
		{"Log level: error", logLevelError, log.ERROR},
		{"Log level: warn", logLevelWarn, log.WARNING},
		{"Log level: info", logLevelInfo, log.INFO},
		{"Log level: debug", logLevelDebug, log.DEBUG},
		{"Invalid log level - defaults to info", "invalid log level", log.INFO},
	}

	for _, tt := range tests {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)

		if tt.in != "" {
			args = append(args, "--"+logLevelFlagName, tt.in)
		}

		startCmd.SetArgs(args)
		err := startCmd.Execute()

		require.Nil(t, err)
		require.Equal(t, tt.out, log.GetLevel(""))
	}
}

func TestStartCmdSyncTimeout(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	startCmd.SetArgs(append(requiredArgs(storageTypeMemOption), "--"+syncTimeoutFlagName, "number"))
	err := startCmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "parsing \"number\": invalid syntax")
}

func TestStartCmdWithTLSCertParams(t *testing.T) {
	t.Run("Success with tls-systemcertpool arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+tlsSystemCertPoolFlagName, "true")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Nil(t, err)
	})

	t.Run("Fail with invalid tls-systemcertpool arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+tlsSystemCertPoolFlagName, "invalid")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
	})

	t.Run("Failed to read cert", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+tlsCACertsFlagName, "/test/path")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Contains(t, err.Error(), "failed to read cert: open /test/path")
	})
}

func TestStartCmdEmptyDomain(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	args := requiredArgs(storageTypeMemOption)
	args = append(args, "--"+didDomainFlagName, "")

	startCmd.SetArgs(args)

	err := startCmd.Execute()
	require.EqualError(t, err, "did-domain value is empty")
}

func TestStartCmdWithSecretLockKeyPathParam(t *testing.T) {
	t.Run("Success with valid key file", func(t *testing.T) {
		file, closeFunc := createKeyFile(t, false)
		defer closeFunc()

		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+secretLockKeyPathFlagName, file)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Fail with invalid key file content", func(t *testing.T) {
		file, closeFunc := createKeyFile(t, true)
		defer closeFunc()

		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+secretLockKeyPathFlagName, file)

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
	})

	t.Run("Fail with invalid secret-lock-key-path arg", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+secretLockKeyPathFlagName, "invalid")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
	})
}

func TestStartCmdWithHubAuthURLParam(t *testing.T) {
	startCmd := GetStartCmd(&mockServer{})

	args := requiredArgs(storageTypeMemOption)
	args = append(args, "--"+hubAuthURLFlagName, "http://example.com")

	startCmd.SetArgs(args)

	err := startCmd.Execute()
	require.NoError(t, err)
}

func TestStartCmdWithEnableCORSParam(t *testing.T) {
	t.Run("Success with CORS enabled", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+enableCORSFlagName, "true")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Fail with invalid enable-cors param", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+enableCORSFlagName, "invalid")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
	})
}

func TestStartCmdWithCacheExpirationParam(t *testing.T) {
	t.Run("Success with cache-expiration set", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+cacheExpirationFlagName, "10m")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Fail with invalid cache-expiration duration string", func(t *testing.T) {
		startCmd := GetStartCmd(&mockServer{})

		args := requiredArgs(storageTypeMemOption)
		args = append(args, "--"+cacheExpirationFlagName, "invalid")

		startCmd.SetArgs(args)

		err := startCmd.Execute()
		require.Error(t, err)
	})
}

func TestStartKMSService(t *testing.T) {
	const invalidStorageOption = "invalid"

	t.Run("Success with default args", func(t *testing.T) {
		params := kmsRestParams(t)

		err := startKmsService(params, &mockServer{})
		require.NoError(t, err)
	})

	t.Run("Fail with invalid storage option", func(t *testing.T) {
		params := kmsRestParams(t)
		params.storageParams.storageType = invalidStorageOption

		err := startKmsService(params, &mockServer{})
		require.Error(t, err)
	})
}

func TestStartMetrics(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv := &mockServer{}

		startMetrics(srv, "localhost:8081")

		logger, ok := srv.Logger().(*mocklogger.MockLogger)
		require.True(t, ok)
		require.Empty(t, logger.FatalLogContents)
	})
}

func requiredArgs(databaseType string) []string {
	args := []string{
		"--" + hostURLFlagName, "localhost:8080",
		"--" + databaseTypeFlagName, databaseType,
		"--" + userKeysStorageTypeFlagName, databaseType,
	}

	if databaseType == storageTypeMongoDBOption {
		args = append(args,
			"--"+databaseURLFlagName, "mongodb://localhost:27017",
			"--"+userKeysStorageURLFlagName, "mongodb://localhost:27017")
	}

	return args
}

func buildAllArgsWithOneBlank(flags []string, blankArg string) []string {
	var args []string

	for _, f := range flags {
		if f == blankArg {
			args = append(args, "--"+f, "")

			continue
		}

		args = append(args, "--"+f, "value")
	}

	return args
}

func kmsRestParams(t *testing.T) *kmsRestParameters {
	t.Helper()

	startCmd := GetStartCmd(&mockServer{})

	err := startCmd.ParseFlags(requiredArgs(storageTypeMemOption))
	require.NoError(t, err)

	params, err := getKmsRestParameters(startCmd)
	require.NotNil(t, params)
	require.NoError(t, err)

	return params
}

func setEnvVars(t *testing.T) {
	t.Helper()

	err := os.Setenv(hostURLEnvKey, "localhost:8080")
	require.NoError(t, err)

	err = os.Setenv(databaseTypeEnvKey, storageTypeMemOption)
	require.NoError(t, err)

	err = os.Setenv(userKeysStorageTypeEnvKey, storageTypeMemOption)
	require.NoError(t, err)
}

func unsetEnvVars(t *testing.T) {
	t.Helper()

	err := os.Unsetenv(hostURLEnvKey)
	require.NoError(t, err)

	err = os.Unsetenv(databaseTypeEnvKey)
	require.NoError(t, err)

	err = os.Unsetenv(userKeysStorageTypeEnvKey)
	require.NoError(t, err)
}

func checkFlagPropertiesCorrect(t *testing.T, cmd *cobra.Command, flagName, flagShorthand, flagUsage string) {
	t.Helper()

	flag := cmd.Flag(flagName)

	require.NotNil(t, flag)
	require.Equal(t, flagName, flag.Name)
	require.Equal(t, flagShorthand, flag.Shorthand)
	require.Equal(t, flagUsage, flag.Usage)
	require.Equal(t, "", flag.Value.String())

	flagAnnotations := flag.Annotations
	require.Nil(t, flagAnnotations)
}

func createKeyFile(t *testing.T, empty bool) (string, func()) {
	t.Helper()

	f, err := ioutil.TempFile("", "secret-lock.key")
	require.NoError(t, err)

	closeFunc := func() {
		require.NoError(t, f.Close())
		require.NoError(t, os.Remove(f.Name()))
	}

	if empty {
		return f.Name(), closeFunc
	}

	key := make([]byte, sha256.Size)
	_, err = rand.Read(key)
	require.NoError(t, err)

	encodedKey := make([]byte, base64.URLEncoding.EncodedLen(len(key)))
	base64.URLEncoding.Encode(encodedKey, key)

	n, err := f.Write(encodedKey)
	require.NoError(t, err)
	require.Equal(t, len(encodedKey), n)

	return f.Name(), closeFunc
}
