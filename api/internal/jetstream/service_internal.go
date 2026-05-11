package jetstream

import (
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func generateUserJWTForAccount(accountSigningSeed, accountPublicKey string) (userJWT string, userSeed []byte, err error) {
	accountKP, err := nkeys.FromSeed([]byte(accountSigningSeed))
	if err != nil {
		return "", nil, err
	}
	userKP, err := nkeys.CreateUser()
	if err != nil {
		return "", nil, err
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return "", nil, err
	}
	claims := natsjwt.NewUserClaims(userPub)
	claims.Name = "console-api-js"
	signerPub, err := accountKP.PublicKey()
	if err != nil {
		return "", nil, err
	}
	if signerPub != accountPublicKey {
		claims.IssuerAccount = accountPublicKey
	}
	claims.IssuedAt = time.Now().Unix()
	userJWT, err = claims.Encode(accountKP)
	if err != nil {
		return "", nil, err
	}
	userSeed, err = userKP.Seed()
	if err != nil {
		return "", nil, err
	}
	return userJWT, userSeed, nil
}
