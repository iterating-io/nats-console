package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"

	"github.com/iterating-io/nats-console/api/internal/auth"
	"github.com/iterating-io/nats-console/api/internal/config"
	"github.com/iterating-io/nats-console/api/internal/httpapi"
	"github.com/iterating-io/nats-console/api/internal/store"
)

// extractOperatorPublicKey derives the public key from an operator NKey seed.
func extractOperatorPublicKey(operatorSeed string) (string, error) {
	if operatorSeed == "" {
		return "", errors.New("OPERATOR_NKEY is required")
	}
	kp, err := nkeys.FromSeed([]byte(operatorSeed))
	if err != nil {
		return "", err
	}
	return kp.PublicKey()
}

func main() {
	cfg := config.Load()

	// Extract operator public key for JWT signing
	operatorPubKey, err := extractOperatorPublicKey(cfg.OperatorNKey)
	if err != nil {
		log.Fatalf("extract operator public key: %v", err)
	}
	jwtSvc := auth.NewService(operatorPubKey)

	st, err := store.New(config.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	server := httpapi.New(cfg, jwtSvc, nil, st)

	connectNATS := func() *nats.Conn {
		opts := []nats.Option{
			nats.Name("nats-console-api"),
			nats.Timeout(5 * time.Second),
			nats.ReconnectWait(3 * time.Second),
			nats.MaxReconnects(-1),
			nats.RetryOnFailedConnect(true),
		}
		if cfg.NATSSysNKey != "" {
			userJWT, userSeed, err := generateUserJWT(cfg.NATSSysNKey)
			if err != nil {
				log.Printf("generate sys user JWT: %v", err)
			} else {
				opts = append(opts,
					nats.UserJWT(
						func() (string, error) { return userJWT, nil },
						func(nonce []byte) ([]byte, error) {
							kp, err := nkeys.FromSeed(userSeed)
							if err != nil {
								return nil, err
							}
							return kp.Sign(nonce)
						},
					),
				)
			}
		}
		opts = append(opts,
			nats.ConnectHandler(func(nc *nats.Conn) {
				log.Printf("NATS connected: %s", nc.ConnectedUrl())
				server.SetNATSConn(nc)
				server.RefreshNATSCapabilities()
				server.LoadFromNATS()
			}),
			nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
				if err != nil {
					log.Printf("NATS disconnected: %v", err)
				}
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				log.Printf("NATS reconnected: %s", nc.ConnectedUrl())
				server.RefreshNATSCapabilities()
				server.LoadFromNATS()
			}),
			nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
				log.Printf("NATS error: %v", err)
			}),
			nats.ClosedHandler(func(_ *nats.Conn) {
				log.Printf("NATS connection closed")
			}),
		)
		nc, err := nats.Connect(cfg.NATSURL, opts...)
		if err != nil {
			log.Fatalf("NATS connect: %v", err)
		}
		return nc
	}

	natsConn := connectNATS()
	defer natsConn.Close()
	server.SetNATSConn(natsConn)
	if natsConn.IsConnected() {
		log.Printf("NATS connected: %s", natsConn.ConnectedUrl())
	}

	addr := ":" + config.APIPort
	log.Printf("API listening on %s (NATS: %s)", addr, cfg.NATSURL)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// generateUserJWT creates a fresh user keypair, signs a user JWT with the given
// sys account seed, and returns the JWT string and the user seed.
// This is called once at startup so no user credentials need to be stored in .env.
func generateUserJWT(sysAccountSeed string) (userJWT string, userSeed []byte, err error) {
	accountKP, err := nkeys.FromSeed([]byte(sysAccountSeed))
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
	claims.Name = "console-api"
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
