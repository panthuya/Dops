package cmd

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pavlo-v-chernykh/keystore-go/v4"
	"github.com/spf13/cobra"
)

var (
	certCmd = &cobra.Command{
		Use:   "cert [file]",
		Short: "Certificate operations (checks if a file contains a full chain)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filename := args[0]
			ext := strings.ToLower(filepath.Ext(filename))

			switch ext {
			case ".pem", ".crt":
				checkPEMChain(filename)
			case ".jks":
				checkJKSChain(filename, jksPassword)
			default:
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("Unsupported file extension. Only .pem, .crt, and .jks are supported."))
			}
		},
	}

	jksPassword string
)

func init() {
	rootCmd.AddCommand(certCmd)
	certCmd.Flags().StringVarP(&jksPassword, "password", "p", "", "Password for JKS file (default empty)")
}

func checkPEMChain(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error reading file: %v\n", err)))
		return
	}

	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				continue
			}
			certs = append(certs, cert)
		}
	}

	analyzeCerts(filename, certs)
}

func checkJKSChain(filename, password string) {
	f, err := os.Open(filename)
	if err != nil {
		fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error opening JKS file: %v\n", err)))
		return
	}
	defer f.Close()

	ks := keystore.New()
	err = ks.Load(f, []byte(password))
	if err != nil {
		fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error decoding JKS (maybe wrong password? use -p): %v\n", err)))
		return
	}

	foundChain := false
	for _, alias := range ks.Aliases() {
		if !ks.IsPrivateKeyEntry(alias) {
			continue
		}

		chain, err := ks.GetPrivateKeyEntryCertificateChain(alias)
		if err != nil {
			continue
		}

		var certs []*x509.Certificate
		for _, certBytes := range chain {
			cert, err := x509.ParseCertificate(certBytes.Content)
			if err == nil {
				certs = append(certs, cert)
			}
		}

		analyzeCerts(fmt.Sprintf("%s (alias: %s)", filename, alias), certs)
		foundChain = true
	}

	if !foundChain {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("No private key entries found in JKS. A full chain is typically stored with a private key."))
	}
}

func analyzeCerts(name string, certs []*x509.Certificate) {
	if len(certs) == 0 {
		fmt.Printf(" ❌ %s: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(name), "No certificates found")
		return
	}

	if len(certs) == 1 {
		fmt.Printf(" ❌ %s: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(name), "NOT a full chain (Only a single certificate found)")
		fmt.Printf("    Subject: %s\n", certs[0].Subject.CommonName)
		fmt.Printf("    Issuer: %s\n", certs[0].Issuer.CommonName)
		return
	}

	// Verify if they form a chain
	isChain := true
	for i := 0; i < len(certs)-1; i++ {
		// certs[i] should be issued by certs[i+1]
		err := certs[i].CheckSignatureFrom(certs[i+1])
		if err != nil {
			isChain = false
			break
		}
	}

	if isChain {
		fmt.Printf(" ✔ %s: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Bold(true).Render(name), fmt.Sprintf("FULL CHAIN FOUND (%d certificates)", len(certs)))
		for i, cert := range certs {
			fmt.Printf("    [%d] Subject: %s\n", i, cert.Subject.CommonName)
			fmt.Printf("        Issuer: %s\n", cert.Issuer.CommonName)
		}
	} else {
		fmt.Printf(" ❌ %s: %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(name), fmt.Sprintf("NOT a full chain (Multiple certificates found (%d), but they do not form a sequential chain)", len(certs)))
		for i, cert := range certs {
			fmt.Printf("    [%d] Subject: %s\n", i, cert.Subject.CommonName)
			fmt.Printf("        Issuer: %s\n", cert.Issuer.CommonName)
		}
	}
}
