package certificate

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/pkg/errors"
	stepx509 "github.com/smallstep/cli/crypto/certificates/x509"
	"github.com/smallstep/cli/crypto/keys"
	"github.com/smallstep/cli/errs"
	"github.com/smallstep/cli/utils/reader"
	"github.com/urfave/cli"
)

func createCommand() cli.Command {
	return cli.Command{
		Name:   "create",
		Action: cli.ActionFunc(createAction),
		Usage:  "create a certificate or certificate signing request",
		UsageText: `**step certificate create** <subject> <crt_file> <key_file>
		[**--csr**] [**--type**=<certificate_type>] [**--profile**=<profile>]`,
		Description: `**step certificate create** generates a certificate or a
certificate signing requests (CSR) that can be signed later using 'step
certificates sign' (or some other tool) to produce a certificate.

This command creates x.509 certificates for use with TLS.

## POSITIONAL ARGUMENTS

<subject>
: The subject of the certificate. Typically this is a hostname for services or an email address for people.

<crt_file>
: File to write CRT or CSR to (PEM format)

<key_file>
: File to write private key to (PEM format)

## EXIT CODES

This command returns 0 on success and \>0 if any error occurs.
`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "type",
				Value: "x509",
				Usage: `The type of certificate to generate. If not specified default is x509.

: <type> is a case-sensitive string and must be one of:
    **x509**
    : Generate an x.509 certificate suitable for use with TLS.`,
			},
			cli.StringFlag{
				Name:  "profile",
				Value: "leaf",
				Usage: `The certificate profile sets various certificate details such as
  certificate use and expiration. The default profile is 'leaf' which is suitable
  for a client or server using TLS.

: <profile> is a case-sensitive string and must be one of:
    **leaf**
	:  Generate a leaf x.509 certificate suitable for use with TLs.

    **intermediate-ca**
    :  Generate a certificate that can be used to sign additional leaf or intermediate certificates.

    **root-ca**
    :  Generate a new self-signed root certificate suitable for use as a root CA.`,
			},
			cli.BoolFlag{
				Name:  "csr",
				Usage: `Generate a certificate signing request (CSR) instead of a certificate.`,
			},
			cli.StringFlag{
				Name:  "ca",
				Usage: `The certificate authority used to issue the new certificate (PEM file).`,
			},
			cli.StringFlag{
				Name:  "ca-key",
				Usage: `The certificate authority private key used to sign the new certificate (PEM file).`,
			},
			cli.BoolFlag{
				Name: "no-password",
				Usage: `Do not ask for a password to encrypt the private key.
Sensitive key material will be written to disk unencrypted. This is not
recommended. Requires **--insecure** flag.`,
			},
			cli.BoolFlag{
				Name:   "insecure",
				Hidden: true,
			},
		},
	}
}

func createAction(ctx *cli.Context) error {
	if err := errs.NumberOfArguments(ctx, 3); err != nil {
		return err
	}

	// Use password to protect private JWK by default
	usePassword := true
	if ctx.Bool("no-password") {
		if ctx.Bool("insecure") {
			usePassword = false
		} else {
			return errs.RequiredWithFlag(ctx, "insecure", "no-password")
		}
	}

	subject := ctx.Args().Get(0)
	crtFile := ctx.Args().Get(1)
	keyFile := ctx.Args().Get(2)
	if crtFile == keyFile {
		return errs.EqualArguments(ctx, "CRT_FILE", "KEY_FILE")
	}

	typ := ctx.String("type")
	prof := ctx.String("profile")
	caPath := ctx.String("ca")
	caKeyPath := ctx.String("ca-key")
	if ctx.Bool("csr") {
		typ = "x509-csr"
	}

	var (
		err    error
		priv   interface{}
		pubPEM *pem.Block
	)
	switch typ {
	case "x509-csr":
		priv, err = keys.GenerateDefaultKey()
		if err != nil {
			return errors.WithStack(err)
		}

		_csr := &x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName: subject,
			},
		}
		csrBytes, err := x509.CreateCertificateRequest(rand.Reader, _csr, priv)
		if err != nil {
			return errors.WithStack(err)
		}

		pubPEM = &pem.Block{
			Type:    "CSR",
			Bytes:   csrBytes,
			Headers: map[string]string{},
		}
	case "x509":
		var (
			err     error
			profile stepx509.Profile
		)
		switch prof {
		case "leaf":
			issIdentity, err := loadIssuerIdentity(prof, caPath, caKeyPath)
			if err != nil {
				return errors.WithStack(err)
			}
			profile, err = stepx509.NewLeafProfile(subject, issIdentity.Crt,
				issIdentity.Key)
			if err != nil {
				return errors.WithStack(err)
			}
		case "intermediate-ca":
			issIdentity, err := loadIssuerIdentity(prof, caPath, caKeyPath)
			if err != nil {
				return errors.WithStack(err)
			}
			if err != nil {
				return errors.WithStack(err)
			}
			profile, err = stepx509.NewIntermediateProfile(subject,
				issIdentity.Crt, issIdentity.Key)
			if err != nil {
				return errors.WithStack(err)
			}
		case "root-ca":
			profile, err = stepx509.NewRootProfile(subject)
			if err != nil {
				return errors.WithStack(err)
			}
		default:
			return errs.InvalidFlagValue(ctx, "profile", prof, "leaf, intermediate-ca, root-ca")
		}
		crtBytes, err := profile.CreateCertificate()
		if err != nil {
			return errors.WithStack(err)
		}
		pubPEM = &pem.Block{
			Type:    "CERTIFICATE",
			Bytes:   crtBytes,
			Headers: map[string]string{},
		}
		priv = profile.SubjectPrivateKey()
	case "ssh":
		return errors.Errorf("implementation incomplete! Come back later ...")
	default:
		return errs.InvalidFlagValue(ctx, "type", typ, "x509, ssh")
	}

	if err := ioutil.WriteFile(crtFile, pem.EncodeToMemory(pubPEM),
		os.FileMode(0600)); err != nil {
		return errs.FileError(err, crtFile)
	}

	var pass string
	if usePassword {
		if err := reader.ReadPasswordSubtle(
			fmt.Sprintf("Password with which to encrypt private key file `%s`: ", keyFile),
			&pass, "Password", reader.RetryOnEmpty); err != nil {
			return errors.WithStack(err)
		}
	}
	if err := keys.WritePrivateKey(priv, pass, keyFile); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func loadIssuerIdentity(profile, caPath, caKeyPath string) (*stepx509.Identity, error) {
	if caPath == "" {
		return nil, errors.Errorf("Missing value for flag '--ca'.\n\nFlags "+
			"'--ca' and '--ca-key' are required when creating a %s x509 Certificate.",
			strings.Title(profile))
	}
	if caKeyPath == "" {
		return nil, errors.Errorf("Missing value for flag '--ca-key'.\n\nFlags "+
			"'--ca' and '--ca-key' are required when creating a %s x509 Certificate.",
			strings.Title(profile))
	}
	return stepx509.LoadIdentityFromDisk(caPath, caKeyPath,
		func() (string, error) {
			var pass string
			if err := reader.ReadPasswordSubtle(
				fmt.Sprintf("Password with which to decrypt CA private key file `%s`: ", caKeyPath),
				&pass, "Password", reader.RetryOnEmpty); err != nil {
				return "", errors.WithStack(err)
			}
			return pass, nil
		})

}
