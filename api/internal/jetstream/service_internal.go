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
	// By default this helper creates a general-purpose ephemeral user. Callers
	// may set permissions directly on the returned JWT claims if needed. For
	// purge operations we will create a more restricted helper below.
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

// generateEphemeralPurgeJWT generates a short-lived user JWT (and seed)
// intended exclusively for performing a purge operation. The returned JWT is
// signed by the account signing key and includes a publish allow only for the
// specified purge subject. The token will be issued with an `exp` short TTL.
func generateEphemeralPurgeJWT(accountSigningSeed, accountPublicKey, purgeSubject string, ttl time.Duration) (userJWT string, userSeed []byte, err error) {
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
	claims.Name = "console-api-purge"
	// Restrict publish to the purge subject only.
	claims.Permissions.Pub.Allow = []string{purgeSubject}
	// Set a short expiry.
	claims.Expires = time.Now().Add(ttl).Unix()
	signerPub, err := accountKP.PublicKey()
	if err != nil {
		return "", nil, err
	}
	if signerPub != accountPublicKey {
		claims.IssuerAccount = accountPublicKey
	}
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
