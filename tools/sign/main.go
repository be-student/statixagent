// sign is the release-signing helper (MVP §5/§7): it generates the ed25519
// keypair, signs checksums.txt, and prints the public key for stamping into
// the agent at build time.
//
//	sign keygen                 print "private=<hex> public=<hex>" (run once, store private in CI secret)
//	sign pubkey                 print the public key for $STATIX_SIGNING_KEY
//	sign file <path>            write <path>.sig signed with $STATIX_SIGNING_KEY
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		check(err)
		fmt.Printf("private=%s\npublic=%s\n", hex.EncodeToString(priv), hex.EncodeToString(pub))
	case "pubkey":
		priv := keyFromEnv()
		fmt.Println(hex.EncodeToString(priv.Public().(ed25519.PublicKey)))
	case "file":
		if len(os.Args) != 3 {
			usage()
		}
		priv := keyFromEnv()
		data, err := os.ReadFile(os.Args[2])
		check(err)
		check(os.WriteFile(os.Args[2]+".sig", ed25519.Sign(priv, data), 0o644))
		fmt.Println("wrote", os.Args[2]+".sig")
	default:
		usage()
	}
}

func keyFromEnv() ed25519.PrivateKey {
	h := os.Getenv("STATIX_SIGNING_KEY")
	if h == "" {
		fmt.Fprintln(os.Stderr, "STATIX_SIGNING_KEY (hex ed25519 private key) is not set")
		os.Exit(1)
	}
	key, err := hex.DecodeString(h)
	check(err)
	if len(key) != ed25519.PrivateKeySize {
		fmt.Fprintln(os.Stderr, "STATIX_SIGNING_KEY has wrong length")
		os.Exit(1)
	}
	return ed25519.PrivateKey(key)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sign keygen | sign pubkey | sign file <path>")
	os.Exit(2)
}
