package certificate

import (
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"

	"github.com/pkg/errors"
	stepx509 "github.com/smallstep/cli/crypto/certificates/x509"
	"github.com/smallstep/cli/errs"
	"github.com/urfave/cli"
)

func verifyCommand() cli.Command {
	return cli.Command{
		Name:      "verify",
		Action:    cli.ActionFunc(verifyAction),
		Usage:     `verify a certificate.`,
		UsageText: `step certificates verify CRT_FILE [--host=HOST]`,
		Description: `The 'step certificates verify' command executes the certificate path validation
algorithm for x.509 certificates defined in RFC 5280. If the certificate is valid
this command will return '0'. If validation fails, or if an error occurs, this
command will produce a non-zero return value.

  POSITIONAL ARGUMENTS
    CRT_FILE
      The path to a certificate to validate.`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "host",
				Usage: `Check whether the certificate is for the specified host.`,
			},
			cli.StringFlag{
				Name: "roots",
				Usage: `Root certificates to use in the path validation algorithm.

    ROOTS is a string containing a (FILE | LIST of FILES | DIRECTORY) defined in one of the following ways:
      FILE
        Relative or full path to a file. All certificates in the file will be used for path validation.
      LIST of Files
        Comma-separated list of relative or full file paths. Every PEM encoded certificate
        from each file will be used for path validation.
      DIRECTORY
        Relative or full path to a directory. Every PEM encoded certificate from each file
        in the directory will be used for path validation.`,
			},
		},
	}
}

func verifyAction(ctx *cli.Context) error {
	if err := errs.NumberOfArguments(ctx, 1); err != nil {
		return err
	}

	crtFile := ctx.Args().Get(0)
	crtBytes, err := ioutil.ReadFile(crtFile)
	if err != nil {
		return errs.FileError(err, crtFile)
	}

	var (
		crt              *x509.Certificate
		ipems            []byte
		intermediatePool = x509.NewCertPool()
		block            *pem.Block
	)
	// The first certificate PEM in the file is our leaf Certificate.
	// Any certificate after the first is added to the list of Intermediate
	// certificates used for path validation.
	for len(crtBytes) > 0 {
		block, crtBytes = pem.Decode(crtBytes)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if crt == nil {
			crt, err = x509.ParseCertificate(block.Bytes)
			if err != nil {
				return errors.WithStack(err)
			}
		} else {
			ipems = append(ipems, pem.EncodeToMemory(block)...)
		}
	}
	if len(ipems) > 0 && !intermediatePool.AppendCertsFromPEM(ipems) {
		return errors.Errorf("failure creating intermediate list from certificate '%s'", crtFile)
	}

	var (
		host     = ctx.String("host")
		roots    = ctx.String("roots")
		rootPool *x509.CertPool
	)

	if roots != "" {
		rootPool, err = stepx509.ReadCertPool(roots)
		if err != nil {
			errors.Wrapf(err, "failure to load root certificate pool from input path '%s'", roots)
		}
	}

	opts := x509.VerifyOptions{
		DNSName:       host,
		Roots:         rootPool,
		Intermediates: intermediatePool,
	}

	if _, err := crt.Verify(opts); err != nil {
		return errors.Wrapf(err, "failed to verify certificate")
	}

	return nil
}
