package cli

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func (o *options) encryptionCommand() *cobra.Command {
	root := &cobra.Command{Use: "encryption", GroupID: "configuration", Short: "Manage client-side backup encryption keys", Long: "Generate private wrapping keys for client-side encrypted backups.\nConfigure key IDs and environment variable references in YAML.", Example: "  dbvault encryption keygen --file key-v1.txt", Args: noPositionalArgs}
	var path string
	keygen := &cobra.Command{Use: "keygen --file <new-file>", Short: "Generate a random 256-bit wrapping key in a NEW private file", Args: noPositionalArgs,
		Long: "Create a base64-encoded 32-byte wrapping key. Existing files are refused.\nLoad the file into an environment variable referenced by encryption.keys.\nKeep previous key IDs available until all backups using them have expired.\nThe key is never printed in command output.", Example: "  dbvault encryption keygen --file key-v1.txt", RunE: func(c *cobra.Command, _ []string) error {
			if path == "" {
				return fmt.Errorf("--file is required")
			}
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, err = f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
			if err == nil {
				err = f.Sync()
			}
			err = errors.Join(err, f.Close())
			if err != nil {
				os.Remove(path)
				return err
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			return o.output(c, map[string]string{"key_file": absolute, "status": "created"})
		}}
	keygen.Flags().StringVar(&path, "file", "", "NEW private key file (never overwrite)")
	root.AddCommand(keygen)
	return root
}
