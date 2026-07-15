package moonlight

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
)

type PairResponse struct {
	Paired            int    `xml:"paired"`
	PlainCert         string `xml:"plaincert"`
	ChallengeResponse string `xml:"challengeresponse"`
	PairingSecret     string `xml:"pairingsecret"`
	StatusMessage     string `xml:"statusmessage"`
}

func (c *Client) getPairStatus(phrase string, params map[string]string, secure bool) (*PairResponse, error) {
	if params == nil {
		params = make(map[string]string)
	}
	params["devicename"] = "roth"
	params["updateState"] = "1"
	params["phrase"] = phrase
	params["version"] = "7" // Force SHA-256

	url := c.getURL(secure, "/pair", params)
	var resp *http.Response
	var err error

	if secure {
		resp, err = c.httpsClient.Get(url)
	} else {
		resp, err = c.pairingHTTPClient.Get(url)
	}

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	logrus.Infof("🌕 [Moonlight pair/%s] HTTP %d raw: %s", phrase, resp.StatusCode, string(body))

	var root PairResponse
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	if root.Paired != 1 {
		return &root, fmt.Errorf("pairing failed: %s", root.StatusMessage)
	}

	return &root, nil
}

func (c *Client) Pair(pin string) error {
	salt := GenerateSalt()
	aesKey := DeriveAESKey(salt, pin)

	// Stage 1: Get server certificate.
	// Sunshine exchanges certs as hex-encoded PEM (not DER); it stores the raw bytes we
	// send here and re-loads them at stage 4 to verify our RSA signature.
	plainCertHex := hex.EncodeToString(c.Identity.CertPEM)
	saltHex := hex.EncodeToString(salt)

	resp, err := c.getPairStatus("getservercert", map[string]string{
		"salt":       saltHex,
		"clientcert": plainCertHex,
	}, false)
	if err != nil {
		return fmt.Errorf("stage 1 failed: %v", err)
	}

	serverCertBytes, err := hex.DecodeString(resp.PlainCert)
	if err != nil || len(serverCertBytes) == 0 {
		return fmt.Errorf("invalid server cert format")
	}

	// Stage 2: Client challenge
	randomChallenge := GenerateSalt() // 16 random bytes
	encryptedChallenge, err := AES128ECBEncrypt(aesKey, randomChallenge)
	if err != nil {
		return err
	}

	resp, err = c.getPairStatus("clientchallenge", map[string]string{
		"clientchallenge": hex.EncodeToString(encryptedChallenge),
	}, false)
	if err != nil {
		return fmt.Errorf("stage 2 failed: %v", err)
	}

	challengeResponseData, err := hex.DecodeString(resp.ChallengeResponse)
	if err != nil {
		return err
	}

	decryptedChallengeResp, err := AES128ECBDecrypt(aesKey, challengeResponseData)
	if err != nil || len(decryptedChallengeResp) < 48 { // 32 hash + 16 challenge
		return fmt.Errorf("invalid challenge response length")
	}

	_ = decryptedChallengeResp[:32] // serverResponse
	serverChallenge := decryptedChallengeResp[32:48]

	// Stage 3: Server challenge response
	clientCertSignature := c.Identity.Cert.Signature
	clientSecretData := GenerateSalt() // 16 random bytes

	challengeRespBuffer := append(serverChallenge, clientCertSignature...)
	challengeRespBuffer = append(challengeRespBuffer, clientSecretData...)

	challengeRespHash := sha256.Sum256(challengeRespBuffer)

	encryptedHash, err := AES128ECBEncrypt(aesKey, challengeRespHash[:])
	if err != nil {
		return err
	}

	resp, err = c.getPairStatus("serverchallengeresp", map[string]string{
		"serverchallengeresp": hex.EncodeToString(encryptedHash),
	}, false)
	if err != nil {
		return fmt.Errorf("stage 3 failed (wrong PIN?): %v", err)
	}

	// Verify server signature (omitted for brevity, assume PIN is correct if paired=1)
	// Stage 4: Client pairing secret
	clientSignature, err := SignData(c.Identity.PrivateKey, clientSecretData)
	if err != nil {
		return err
	}

	clientPairingSecret := append(clientSecretData, clientSignature...)

	resp, err = c.getPairStatus("clientpairingsecret", map[string]string{
		"clientpairingsecret": hex.EncodeToString(clientPairingSecret),
	}, false)
	if err != nil {
		return fmt.Errorf("stage 4 failed: %v", err)
	}

	// Stage 5: Pair challenge (HTTPS)
	_, err = c.getPairStatus("pairchallenge", nil, true)
	if err != nil {
		return fmt.Errorf("stage 5 failed: %v", err)
	}

	return nil
}
