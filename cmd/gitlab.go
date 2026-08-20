package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	gitlabToken string
	gitlabURL   string

	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

var gitlabCmd = &cobra.Command{
	Use:   "gitlab",
	Short: "GitLab related operations",
}

var checkTokenCmd = &cobra.Command{
	Use:   "check-token",
	Short: "Check GitLab token expire time, user, and permissions",
	Run: func(cmd *cobra.Command, args []string) {
		token := gitlabToken
		if token == "" {
			token = os.Getenv("GITLAB_TOKEN")
		}
		if token == "" {
			fmt.Println(errorStyle.Render("Error: GitLab token is required. Use --token flag or GITLAB_TOKEN env var."))
			return
		}

		// First, get token info
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v4/personal_access_tokens/self", gitlabURL), nil)
		if err != nil {
			fmt.Println(errorStyle.Render("Failed to create request: " + err.Error()))
			return
		}
		req.Header.Set("PRIVATE-TOKEN", token)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println(errorStyle.Render("Failed to connect to GitLab: " + err.Error()))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Println(errorStyle.Render(fmt.Sprintf("API returned status: %s", resp.Status)))
			body, _ := io.ReadAll(resp.Body)
			fmt.Println(string(body))
			return
		}

		var tokenData map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&tokenData)

		reqUser, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v4/user", gitlabURL), nil)
		reqUser.Header.Set("PRIVATE-TOKEN", token)
		respUser, err := client.Do(reqUser)
		var userData map[string]interface{}
		if err == nil && respUser.StatusCode == http.StatusOK {
			defer respUser.Body.Close()
			json.NewDecoder(respUser.Body).Decode(&userData)
		}

		// UI/UX Optimization: Render token info in a clean, rounded box
		content := ""
		content += fmt.Sprintf("%s %s\n", keyStyle.Render("Name:      "), valueStyle.Render(fmt.Sprint(tokenData["name"])))
		
		if userData != nil {
			content += fmt.Sprintf("%s %s (@%s)\n", keyStyle.Render("Owner:     "), valueStyle.Render(fmt.Sprint(userData["name"])), valueStyle.Render(fmt.Sprint(userData["username"])))
		}

		content += fmt.Sprintf("%s %s\n", keyStyle.Render("Expires:   "), valueStyle.Render(fmt.Sprint(tokenData["expires_at"])))
		content += fmt.Sprintf("%s %s\n", keyStyle.Render("Revoked:   "), valueStyle.Render(fmt.Sprint(tokenData["revoked"])))
		
		if scopes, ok := tokenData["scopes"].([]interface{}); ok {
			var scopeStrings []string
			for _, s := range scopes {
				scopeStrings = append(scopeStrings, fmt.Sprint(s))
			}
			scopeStr := strings.Join(scopeStrings, ", ")
			
			// Wrap the scopes to max 80 chars, and align it perfectly to the right of "Scopes:"
			valBlock := lipgloss.NewStyle().Width(80).Render(scopeStr)
			row := lipgloss.JoinHorizontal(lipgloss.Top, keyStyle.Render("Scopes:    "), valueStyle.Render(valBlock))
			content += row + "\n"
		}

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FFFF")).
			Padding(0, 2).
			MarginTop(1).
			MarginBottom(1)

		fmt.Println(successStyle.Render("\n✔ Token is Valid!"))
		fmt.Println(box.Render(content))
	},
}

func init() {
	rootCmd.AddCommand(gitlabCmd)
	gitlabCmd.AddCommand(checkTokenCmd)

	gitlabCmd.PersistentFlags().StringVarP(&gitlabToken, "token", "t", "", "GitLab personal access token")
	gitlabCmd.PersistentFlags().StringVarP(&gitlabURL, "url", "u", "https://gitlab.com", "GitLab instance URL")
}
