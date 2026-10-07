// Package imgsig verifies the image an A/B update writes to its inactive root.
//
// The guarantee is update-time, not boot-time (docs/IMMUTABILITY.md, step 3):
// before a slot is written, the artifact is hashed and its detached Ed25519
// signature checked against a trusted public key. A mismatch means the slot is
// not written, and the running system is untouched.
//
// The format is deliberately small and self-contained: no dependency, no
// network, no key server. Keys are Ed25519 public keys in PEM -- what
// `openssl genpkey -algorithm ed25519` writes -- and a signature is a short text
// file, Signature, carrying the artifact's SHA-256 and the 64-byte signature
// over a domain-separated message. Verification streams the artifact: an image
// is hundreds of megabytes and never fits in memory on a node.
package imgsig
