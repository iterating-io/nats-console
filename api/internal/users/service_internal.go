package users

import (
	"fmt"
	"strings"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func buildUserCreds(name, userPublicKey, userSeed, accountSigningSeed, accountPublicKey string) (string, error) {
	accountKP, err := nkeys.FromSeed([]byte(accountSigningSeed))
	if err != nil {
		return "", fmt.Errorf("account signing seed: %w", err)
	}
	claims := natsjwt.NewUserClaims(userPublicKey)
	claims.Name = name
	signerPub, err := accountKP.PublicKey()
	if err != nil {
		return "", fmt.Errorf("account signing public key: %w", err)
	}
	if signerPub != accountPublicKey {
		claims.IssuerAccount = accountPublicKey
	}
	claims.IssuedAt = time.Now().Unix()
	jwt, err := claims.Encode(accountKP)
	if err != nil {
		return "", fmt.Errorf("user jwt: %w", err)
	}
	return strings.Join([]string{
		"-----BEGIN NATS USER JWT-----",
		jwt,
		"------END NATS USER JWT------",
		"",
		"************************* IMPORTANT *************************",
		"NKEY Seed printed below can be used to sign and prove identity.",
		"NKEYs are sensitive and should be treated as secrets.",
		"*************************************************************",
		"",
		"-----BEGIN USER NKEY SEED-----",
		userSeed,
		"------END USER NKEY SEED------",
		"",
	}, "\n"), nil
}
