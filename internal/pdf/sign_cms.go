package pdf

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// This file builds the CMS SignedData a PDF signature holds (RFC 5652, in the
// shape ISO 32000 calls adbe.pkcs7.detached). The standard library can verify
// one but not produce one, and the structure is small and fixed, so it is
// written here rather than adding a dependency for it.

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA256WithRSA = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidECDSAWithSHA2 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type cmsAttribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type signerInfo struct {
	Version         int
	Sid             issuerAndSerial
	DigestAlgorithm algorithmIdentifier
	SignedAttrs     asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlg    algorithmIdentifier
	Signature       []byte
}

type encapContent struct {
	ContentType asn1.ObjectIdentifier
}

type signedData struct {
	Version          int
	DigestAlgorithms []algorithmIdentifier `asn1:"set"`
	Content          encapContent
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     signedData `asn1:"explicit,tag:0"`
}

// null is the ASN.1 NULL that RSA algorithm identifiers carry as parameters.
var null = asn1.RawValue{Tag: asn1.TagNull}

func rawOf(v any) (asn1.RawValue, error) {
	der, err := asn1.Marshal(v)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("pdf: encode a signature field: %w", err)
	}

	return asn1.RawValue{FullBytes: der}, nil
}

// buildCMS signs digest, the SHA-256 of the document's signed byte ranges,
// and returns the DER of the SignedData.
//
// The signature covers the signed attributes rather than the digest itself,
// as CMS requires: the message-digest attribute carries the document's hash,
// and the signer signs the attributes that carry it.
func buildCMS(
	digest []byte,
	key crypto.Signer,
	cert *x509.Certificate,
	chain []*x509.Certificate,
	at time.Time,
) ([]byte, error) {
	var sigAlg algorithmIdentifier

	switch key.Public().(type) {
	case *rsa.PublicKey:
		sigAlg = algorithmIdentifier{Algorithm: oidSHA256WithRSA, Parameters: null}
	case *ecdsa.PublicKey:
		sigAlg = algorithmIdentifier{Algorithm: oidECDSAWithSHA2}
	default:
		return nil, errors.New("pdf: only RSA and ECDSA keys can sign")
	}

	var (
		attrs []cmsAttribute
		err   error
	)

	for _, a := range []struct {
		oid asn1.ObjectIdentifier
		val any
	}{
		{oidContentType, oidData},
		{oidSigningTime, at.UTC()},
		{oidMessageDigest, digest},
	} {
		raw, err := rawOf(a.val)
		if err != nil {
			return nil, err
		}

		attrs = append(attrs, cmsAttribute{Type: a.oid, Values: []asn1.RawValue{raw}})
	}

	// DER sorts a SET OF by encoding, and encoding/asn1 does that for the
	// "set" option, so the bytes signed are the bytes a verifier recomputes.
	set, err := asn1.MarshalWithParams(attrs, "set")
	if err != nil {
		return nil, fmt.Errorf("pdf: encode the signed attributes: %w", err)
	}

	sum := sha256Of(set)

	sig, err := key.Sign(rand.Reader, sum, crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("pdf: sign: %w", err)
	}

	// The attributes are signed as a SET (tag 0x31) and carried as [0]
	// IMPLICIT (tag 0xA0): the same bytes with the tag changed.
	implicit := append([]byte{0xa0}, set[1:]...)

	issuer := asn1.RawValue{FullBytes: cert.RawIssuer}
	digestAlg := algorithmIdentifier{Algorithm: oidSHA256, Parameters: null}

	var certs []byte

	certs = append(certs, cert.Raw...)

	for _, c := range chain {
		certs = append(certs, c.Raw...)
	}

	info := contentInfo{
		ContentType: oidSignedData,
		Content: signedData{
			Version:          1,
			DigestAlgorithms: []algorithmIdentifier{digestAlg},
			Content:          encapContent{ContentType: oidData},
			Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: certs},
			SignerInfos: []signerInfo{{
				Version:         1,
				Sid:             issuerAndSerial{Issuer: issuer, Serial: cert.SerialNumber},
				DigestAlgorithm: digestAlg,
				SignedAttrs:     asn1.RawValue{FullBytes: implicit},
				SignatureAlg:    sigAlg,
				Signature:       sig,
			}},
		},
	}

	der, err := asn1.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("pdf: encode the signature: %w", err)
	}

	return der, nil
}

func sha256Of(b []byte) []byte {
	sum := sha256.Sum256(b)

	return sum[:]
}
