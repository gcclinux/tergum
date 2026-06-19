package cmd

import "github.com/spf13/cobra"

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate from Tergum v2.0 to v3.0",
		Long: `Imports a v2.0 SQLite database and transforms it to v3.0 format.
Optionally rehashes files with BLAKE3 and encrypts existing storage.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromDB, _ := cmd.Flags().GetString("from-db")
			rehash, _ := cmd.Flags().GetBool("rehash")
			encrypt, _ := cmd.Flags().GetBool("encrypt")
			verify, _ := cmd.Flags().GetBool("verify")
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "migrate",
					"from_db": fromDB,
					"rehash":  rehash,
					"encrypt": encrypt,
					"verify":  verify,
				},
				"tergum migrate: migration (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().String("from-db", "", "path to v2.0 SQLite database (required)")
	cmd.Flags().Bool("rehash", false, "compute BLAKE3 hashes replacing MD5")
	cmd.Flags().Bool("encrypt", false, "encrypt existing storage files")
	cmd.Flags().Bool("verify", false, "verify migration integrity")

	return cmd
}
