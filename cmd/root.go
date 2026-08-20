package cmd

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	logoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true).Margin(1)
	infoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

var rootCmd = &cobra.Command{
	Use:   "dops",
	Short: "DOPS - DevOps daily routine CLI",
	Long:  logoStyle.Render(`
 ____                  
|  _ \  ___  _ __  ___ 
| | | |/ _ \| '_ \/ __|
| |_| | (_) | |_) \__ \
|____/ \___/| .__/|___/
            |_|        
`) + "\n" + infoStyle.Render("CLI for DevOps Tasks"),
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}
