// bootstrap generates the base NATS operator setup required for the console API:
//   - Operator NKey
//   - System Account NKey + JWT signed by operator
//   - System User NKey + JWT signed by system account
// JetStream application accounts are created later by the API at runtime based on JS_ACCOUNT_NAME.
//
// Outputs:
//   - deploy/auth.conf   (operator JWT + system account public key + resolver config)
//   - .env              (API env vars including generated NATS credentials)
//   - deploy/keys/      (NKey seed files for backup/re-signing)
//
// Usage:
//
//	go run ./tools/bootstrap [--out-dir ./deploy]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func main() {
	outDir := flag.String("out-dir", "deploy", "directory to write auth.conf and keys")
	printPubkey := flag.String("print-pubkey", "", "print the public key of the given NKey seed file and exit")
	flag.Parse()

	// --print-pubkey mode: read a seed file, print its public key, then exit.
	if *printPubkey != "" {
		seed, err := os.ReadFile(*printPubkey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read seed: %v\n", err)
			os.Exit(1)
		}
		kp, err := nkeys.FromSeed(seed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse seed: %v\n", err)
			os.Exit(1)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "public key: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(pub)
		return
	}

	keysDir := filepath.Join(*outDir, "keys")
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		fatalf("create keys dir: %v", err)
	}

	// --- Operator ---
	operatorKP, err := nkeys.CreateOperator()
	must("create operator keypair", err)
	operatorPub, err := operatorKP.PublicKey()
	must("operator public key", err)
	operatorSeed, err := operatorKP.Seed()
	must("operator seed", err)

	// --- System Account ---
	sysAccountKP, err := nkeys.CreateAccount()
	must("create sys account keypair", err)
	sysAccountPub, err := sysAccountKP.PublicKey()
	must("sys account public key", err)
	sysAccountSeed, err := sysAccountKP.Seed()
	must("sys account seed", err)

	// Sign system account JWT
	sysAccountClaims := jwt.NewAccountClaims(sysAccountPub)
	sysAccountClaims.Name = "SYS"
	sysAccountClaims.IssuedAt = time.Now().Unix()
	sysAccountJWT, err := sysAccountClaims.Encode(operatorKP)
	must("encode sys account JWT", err)

	// --- Operator JWT (references system account) ---
	operatorClaims := jwt.NewOperatorClaims(operatorPub)
	operatorClaims.Name = "nats-console"
	operatorClaims.SystemAccount = sysAccountPub
	operatorClaims.IssuedAt = time.Now().Unix()
	operatorJWT, err := operatorClaims.Encode(operatorKP)
	must("encode operator JWT", err)

	// --- System User ---
	sysUserKP, err := nkeys.CreateUser()
	must("create sys user keypair", err)
	sysUserPub, err := sysUserKP.PublicKey()
	must("sys user public key", err)
	sysUserSeed, err := sysUserKP.Seed()
	must("sys user seed", err)

	// Sign system user JWT with system account key
	sysUserClaims := jwt.NewUserClaims(sysUserPub)
	sysUserClaims.Name = "console-api"
	sysUserClaims.IssuedAt = time.Now().Unix()
	sysUserJWT, err := sysUserClaims.Encode(sysAccountKP)
	must("encode sys user JWT", err)

	// --- Write auth.conf ---
	// resolver_preload embeds the system account JWT so NATS can authenticate
	// system-level connections on first boot before any JWT push is needed.
	authConf := fmt.Sprintf(`operator: %q
system_account: %q

resolver: {
  type: full
  dir: /data/nats/resolver
	allow_delete: true
}

resolver_preload: {
	%s: %q
}
`, operatorJWT, sysAccountPub, sysAccountPub, sysAccountJWT)
	writeFile(filepath.Join(*outDir, "auth.conf"), []byte(authConf), 0644)

	// --- Write root .env ---
	// Contains all local runtime settings plus the generated NATS credentials.
	// Do NOT commit this file — it contains private NKey seeds.
	envPath := filepath.Clean(filepath.Join(*outDir, "..", ".env"))
	envFile := fmt.Sprintf(`# API
NATS_URL=nats://nats:4222
ALLOWED_ORIGINS=http://localhost
NATS_SYS_JWT=%s
NATS_SYS_NKEY=%s
OPERATOR_NKEY=%s
ADMIN_ID=admin
ADMIN_PASSWORD=admin

# WEB
WEB_BASE_PATH=/
`, sysUserJWT, string(sysAccountSeed), string(operatorSeed))
	writeFile(envPath, []byte(envFile), 0600)

	// --- Write seeds to keys/ (backup/recovery only) ---
	writeFile(filepath.Join(keysDir, "sys-account.jwt"), []byte(sysAccountJWT), 0644)
	writeFile(filepath.Join(keysDir, "operator.nk"), operatorSeed, 0600)
	writeFile(filepath.Join(keysDir, "sys-account.nk"), sysAccountSeed, 0600)
	writeFile(filepath.Join(keysDir, "sys-user.nk"), sysUserSeed, 0600)

	fmt.Println("Bootstrap complete.")
	fmt.Printf("  auth.conf        : %s\n", filepath.Join(*outDir, "auth.conf"))
	fmt.Printf("  .env             : %s\n", envPath)
	fmt.Printf("  keys/            : %s\n", keysDir)
	fmt.Printf("  operator pub     : %s\n", operatorPub)
	fmt.Printf("  sys account pub  : %s\n", sysAccountPub)
}

func writeFile(path string, data []byte, perm os.FileMode) {
	if err := os.WriteFile(path, data, perm); err != nil {
		fatalf("write %s: %v", path, err)
	}
	fmt.Printf("  wrote: %s\n", path)
}

func must(label string, err error) {
	if err != nil {
		fatalf("%s: %v", label, err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
