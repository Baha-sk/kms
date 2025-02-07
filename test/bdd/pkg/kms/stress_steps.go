/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package kms

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/greenpau/go-calculator"

	"github.com/trustbloc/kms/test/bdd/pkg/auth"
	"github.com/trustbloc/kms/test/bdd/pkg/internal/bddutil"
)

const (
	userNameTplt = "User%d"
	controller   = "did:example:123456789"
)

func (s *Steps) createUsers(usersNumberEnv string) error {
	usersNumber, err := getUsersNumber(usersNumberEnv)
	if err != nil {
		return err
	}

	for i := 0; i < usersNumber; i++ {
		userName := fmt.Sprintf(userNameTplt, i)

		u := &user{
			name:        userName,
			controller:  controller,
			disableZCAP: true,
		}
		s.users[userName] = u

		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Steps) createUsersFromPrototype(usersNumberEnv, protoUser string) error {
	usersNumber, err := getUsersNumber(usersNumberEnv)
	if err != nil {
		return err
	}

	proto := s.users[protoUser]

	for i := 0; i < usersNumber; i++ {
		userName := fmt.Sprintf(userNameTplt, i)

		u := &user{
			name:        proto.name,
			subject:     proto.subject,
			accessToken: proto.accessToken,
			secretShare: proto.secretShare,
		}
		s.users[userName] = u

		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Steps) stressTestLogin(userName, subjectEnv, accessTokenEnv, secretShareEnv string) error {
	s.bddContext.LoginConfig = readLoginConfigFromEnv()

	subject := os.Getenv(subjectEnv)
	if subject == "" {
		return s.storeSecretInHubAuth(userName)
	}

	u := &user{
		name: userName,
	}
	s.users[userName] = u

	u.subject = subject
	u.accessToken = os.Getenv(accessTokenEnv)

	fmt.Printf("user %s, %s, %s", u.subject, os.Getenv(secretShareEnv), u.accessToken)

	secretShare, err := base64.StdEncoding.DecodeString(os.Getenv(secretShareEnv))
	if err != nil {
		return err
	}

	u.secretShare = secretShare

	return nil
}

func (s *Steps) createEDVDataVaultForMultipleUsers(usersNumberEnv string) error {
	usersNumber, err := getUsersNumber(usersNumberEnv)
	if err != nil {
		return err
	}

	for i := 0; i < usersNumber; i++ {
		err = s.createEDVDataVault(fmt.Sprintf(userNameTplt, i))
		if err != nil {
			return err
		}
	}

	return nil
}

//nolint:funlen,gocyclo
func (s *Steps) stressTestForMultipleUsers(
	totalRequestsEnv, storeType, keyType string, signTimes int, concurrencyEnv string) error {
	totalRequests, err := getUsersNumber(totalRequestsEnv)
	if err != nil {
		return err
	}

	concurrencyReq, err := getConcurrencyReq(concurrencyEnv)
	if err != nil {
		return err
	}

	if storeType != "EDV" && storeType != "LocalStorage" {
		return errors.New("invalid store type:" + storeType)
	}

	var edvCapabilities [][]byte

	if storeType == "EDV" {
		for i := 0; i < totalRequests; i++ {
			userName := fmt.Sprintf(userNameTplt, i)

			u := s.users[userName]
			if err := s.createDID(u); err != nil {
				return fmt.Errorf("create did %w", err)
			}
		}

		edvCapabilities = make([][]byte, 0)

		for i := 0; i < totalRequests; i++ {
			userName := fmt.Sprintf(userNameTplt, i)

			u := s.users[userName]

			edvCapability, err := s.createChainCapability(u)
			if err != nil {
				return fmt.Errorf("create chain capability %w", err)
			}

			capabilityBytes, err := json.Marshal(edvCapability)
			if err != nil {
				return err
			}

			edvCapabilities = append(edvCapabilities, capabilityBytes)
		}
	}

	fmt.Printf("totalRequests: %d, concurrencyReq: %d", totalRequests, concurrencyReq)

	createPool := bddutil.NewWorkerPool(concurrencyReq, s.logger)

	createPool.Start()

	for i := 0; i < totalRequests; i++ {
		r := &stressRequest{
			userName:     fmt.Sprintf(userNameTplt, i),
			keyServerURL: s.bddContext.KeyServerURL,
			edvServerURL: s.bddContext.EDVServerURL,
			keyType:      keyType,
			steps:        s,
			signRequests: signTimes,
		}
		if edvCapabilities != nil {
			r.edvCapability = edvCapabilities[i]
		}

		createPool.Submit(r)
	}

	createPool.Stop()

	s.logger.Infof("got created key store %d responses for %d requests", len(createPool.Responses()), totalRequests)

	if len(createPool.Responses()) != totalRequests {
		return fmt.Errorf("expecting created key store %d responses but got %d", totalRequests, len(createPool.Responses()))
	}

	var (
		createKeyStoreHTTPTime []int64
		createKeyHTTPTime      []int64
		signHTTPTime           []int64
		verifyHTTPTime         []int64
	)

	for _, resp := range createPool.Responses() {
		if resp.Err != nil {
			return resp.Err
		}

		perfInfo, ok := resp.Resp.(stressRequestPerfInfo)
		if !ok {
			return fmt.Errorf("invalid stressRequestPerfInfo response")
		}

		createKeyStoreHTTPTime = append(createKeyStoreHTTPTime, perfInfo.createKeyStoreHTTPTime)
		createKeyHTTPTime = append(createKeyHTTPTime, perfInfo.createKeyHTTPTime)
		signHTTPTime = append(signHTTPTime, perfInfo.signHTTPTime)
		verifyHTTPTime = append(verifyHTTPTime, perfInfo.verifyHTTPTime)
	}

	calc := calculator.NewInt64(createKeyStoreHTTPTime)
	fmt.Printf("create key store avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("create key store max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("create key store min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	calc = calculator.NewInt64(createKeyHTTPTime)
	fmt.Printf("create key avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("create key max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("create key min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	calc = calculator.NewInt64(signHTTPTime)
	fmt.Printf("sign avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("sign max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("sign min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	calc = calculator.NewInt64(verifyHTTPTime)
	fmt.Printf("verify avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("verify max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("verify min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	return nil
}

//nolint:funlen
func (s *Steps) authStressTestForMultipleUsers(totalRequestsEnv, userName, concurrencyEnv string) error {
	totalRequests, err := getUsersNumber(totalRequestsEnv)
	if err != nil {
		return err
	}

	concurrencyReq, err := getConcurrencyReq(concurrencyEnv)
	if err != nil {
		return err
	}

	fmt.Printf("totalRequests: %d, concurrencyReq: %d", totalRequests, concurrencyReq)

	createPool := bddutil.NewWorkerPool(concurrencyReq, s.logger)

	createPool.Start()

	for i := 0; i < totalRequests; i++ {
		r := &authStressRequest{
			userName: userName,
			steps:    s,
		}
		createPool.Submit(r)
	}

	createPool.Stop()

	s.logger.Infof("got created key store %d responses for %d requests", len(createPool.Responses()), totalRequests)

	if len(createPool.Responses()) != totalRequests {
		return fmt.Errorf("expecting created key store %d responses but got %d", totalRequests, len(createPool.Responses()))
	}

	var (
		createKeyStoreHTTPTime []int64
		createKeyHTTPTime      []int64
		signHTTPTime           []int64
	)

	for _, resp := range createPool.Responses() {
		if resp.Err != nil {
			return resp.Err
		}

		perfInfo, ok := resp.Resp.(stressRequestPerfInfo)
		if !ok {
			if !ok {
				return fmt.Errorf("invalid stressRequestPerfInfo response")
			}
		}

		createKeyStoreHTTPTime = append(createKeyStoreHTTPTime, perfInfo.createKeyStoreHTTPTime)
		createKeyHTTPTime = append(createKeyHTTPTime, perfInfo.createKeyHTTPTime)
		signHTTPTime = append(signHTTPTime, perfInfo.signHTTPTime)
	}

	calc := calculator.NewInt64(createKeyStoreHTTPTime)
	fmt.Printf("create key store avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("create key store max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("create key store min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	calc = calculator.NewInt64(createKeyHTTPTime)
	fmt.Printf("create key avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("create key max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("create key min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	calc = calculator.NewInt64(signHTTPTime)
	fmt.Printf("sign avg time: %s\n", (time.Duration(calc.Mean().Register.Mean) *
		time.Millisecond).String())
	fmt.Printf("sign max time: %s\n", (time.Duration(calc.Max().Register.MaxValue) *
		time.Millisecond).String())
	fmt.Printf("sign min time: %s\n", (time.Duration(calc.Min().Register.MinValue) *
		time.Millisecond).String())
	fmt.Println("------")

	return nil
}

func getConcurrencyReq(concurrencyEnv string) (int, error) {
	concurrencyReqStr := os.Getenv(concurrencyEnv)
	if concurrencyReqStr == "" {
		concurrencyReqStr = "10"
	}

	return strconv.Atoi(concurrencyReqStr)
}

func getUsersNumber(usersNumberEnv string) (int, error) {
	usersNumberStr := os.Getenv(usersNumberEnv)
	if usersNumberStr == "" {
		usersNumberStr = "10"
	}

	return strconv.Atoi(usersNumberStr)
}

type stressRequest struct {
	userName      string
	edvCapability []byte
	edvServerURL  string
	keyServerURL  string
	keyType       string
	steps         *Steps
	signRequests  int
}

type stressRequestPerfInfo struct {
	createKeyStoreHTTPTime int64
	createKeyHTTPTime      int64
	signHTTPTime           int64
	verifyHTTPTime         int64
}

func (r *stressRequest) Invoke() (interface{}, error) {
	u := r.steps.users[r.userName]

	createReq := &createKeystoreReq{
		Controller: u.controller,
	}

	if r.edvCapability != nil {
		createReq.EDV = &edvOptions{
			VaultURL:   r.edvServerURL + edvBasePath + "/" + u.vaultID,
			Capability: r.edvCapability,
		}
	}

	perfInfo := stressRequestPerfInfo{}

	startTime := time.Now()

	err := r.steps.createKeystoreReq(u, createReq, r.keyServerURL+createKeystoreEndpoint)
	if err != nil {
		return nil, fmt.Errorf("create keystore %w", err)
	}

	perfInfo.createKeyStoreHTTPTime = time.Since(startTime).Milliseconds()

	startTime = time.Now()

	err = r.steps.makeCreateKeyReq(r.userName, r.keyServerURL+keysEndpoint, r.keyType)
	if err != nil {
		return nil, fmt.Errorf("create key %w", err)
	}

	perfInfo.createKeyHTTPTime = time.Since(startTime).Milliseconds()

	message := randomMessage(1024) //nolint:gomnd

	startTime = time.Now()

	for i := 0; i < r.signRequests; i++ {
		err = r.steps.makeSignMessageReq(r.userName, r.keyServerURL+signEndpoint, message)
		if err != nil {
			return nil, fmt.Errorf("sign %w", err)
		}
	}

	perfInfo.signHTTPTime = time.Since(startTime).Milliseconds() / int64(r.signRequests)

	startTime = time.Now()

	err = r.steps.makeVerifySignatureReq(r.userName, r.keyServerURL+verifyEndpoint, "signature", message)
	if err != nil {
		return nil, err
	}

	perfInfo.verifyHTTPTime = time.Since(startTime).Milliseconds()

	return perfInfo, nil
}

type authStressRequest struct {
	userName string
	steps    *Steps
}

func (r *authStressRequest) Invoke() (interface{}, error) {
	u := r.steps.users[r.userName]

	authzUser := &user{
		name:        r.userName,
		subject:     u.subject,
		secretShare: u.secretShare,
		accessToken: u.accessToken,
	}

	perfInfo := stressRequestPerfInfo{}

	startTime := time.Now()

	err := r.steps.createKeystoreAuthzKMS(authzUser)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth keystore: %w", err)
	}

	perfInfo.createKeyStoreHTTPTime = time.Since(startTime).Milliseconds()

	startTime = time.Now()

	err = r.steps.makeCreateKeyReqAuthzKMS(authzUser, r.steps.bddContext.AuthZKeyServerURL+keysEndpoint, "ED25519")
	if err != nil {
		return nil, fmt.Errorf("failed to create auth keystore key: %w", err)
	}

	perfInfo.createKeyHTTPTime = time.Since(startTime).Milliseconds()

	message := randomMessage(1024) //nolint:gomnd

	startTime = time.Now()

	err = r.steps.makeSignMessageReqAuthzKMS(authzUser, r.steps.bddContext.AuthZKeyServerURL+signEndpoint, []byte(message))
	if err != nil {
		return nil, err
	}

	perfInfo.signHTTPTime = time.Since(startTime).Milliseconds()

	return perfInfo, nil
}

func readLoginConfigFromEnv() *auth.LoginConfig {
	return &auth.LoginConfig{
		HubAuthHydraAdminURL:            os.Getenv("KMS_STRESS_HYDRA_ADMIN_URL"),
		HubAuthOIDCProviderURL:          os.Getenv("KMS_STRESS_OIDC_PROVIDER_URL"),
		HubAuthOIDCProviderSelectionURL: os.Getenv("KMS_STRESS_OIDC_PROVIDER_SELECTION_URL"),
		HubAuthSelectOIDCProviderURL:    os.Getenv("KMS_STRESS_SELECT_OIDC_PROVIDER_URL"),
		LoginURL:                        os.Getenv("KMS_STRESS_LOGIN_URL"),
		AuthenticationURL:               os.Getenv("KMS_STRESS_AUTHENTICATION_URL"),
		ConsentURL:                      os.Getenv("KMS_STRESS_CONSENT_URL"),
		AuthorizationURL:                os.Getenv("KMS_STRESS_AUTHORIZATION_URL"),
		OIDCProviderName:                os.Getenv("KMS_STRESS_OIDC_PROVIDER_NAME"),
	}
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") //nolint:gochecknoglobals

func randomMessage(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))] //nolint:gosec
	}

	return string(b)
}
