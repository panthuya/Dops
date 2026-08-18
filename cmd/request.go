package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	method  string
	headers []string
	data    string
	minimal bool

	reqInfoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
	reqDataStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
)

var httpCmd = &cobra.Command{
	Use:   "http [url] [optional-body]",
	Short: "Make an HTTP request easily (auto-detects GET/POST)",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "http://" + url
		}

		bodyData := data
		if len(args) == 2 {
			bodyData = args[1]
		}

		// Support reading body from file if it starts with @
		if strings.HasPrefix(bodyData, "@") {
			b, err := os.ReadFile(strings.TrimPrefix(bodyData, "@"))
			if err == nil {
				bodyData = string(b)
			}
		}

		actualMethod := method
		if bodyData != "" && method == "GET" {
			actualMethod = "POST"
		}

		var bodyReader io.Reader
		if bodyData != "" {
			bodyReader = bytes.NewBuffer([]byte(bodyData))
		}

		req, err := http.NewRequest(actualMethod, url, bodyReader)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			return
		}

		if bodyData != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		for _, h := range headers {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error performing request: %v\n", err)
			return
		}
		defer resp.Body.Close()

		// Status Line
		statusColor := "#00FFFF"
		if resp.StatusCode >= 400 {
			statusColor = "#FF0000"
		} else if resp.StatusCode >= 300 {
			statusColor = "#FFFF00"
		} else if resp.StatusCode >= 200 {
			statusColor = "#4ADE80"
		}
		statusStyle := lipgloss.NewStyle().Background(lipgloss.Color(statusColor)).Foreground(lipgloss.Color("#000000")).Bold(true).Padding(0, 1).MarginTop(1)
		fmt.Println(statusStyle.Render(fmt.Sprintf("%s %s ➜ %s", actualMethod, url, resp.Status)))

		// Headers
		headerKeyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		headerValStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
		headerBlockStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("#444444")).
			PaddingLeft(1).
			MarginTop(1).
			MarginBottom(1).
			Width(100) // Forces long headers (like massive cookies) to wrap neatly

		if minimal {
			var hLines []string
			for k, v := range resp.Header {
				if strings.ToLower(k) == "set-cookie" || strings.ToLower(k) == "location" {
					hLines = append(hLines, fmt.Sprintf("%s: %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Render(k), headerValStyle.Render(strings.Join(v, ", "))))
				}
			}
			if len(hLines) > 0 {
				fmt.Println(headerBlockStyle.Render(strings.Join(hLines, "\n")))
			}
			return
		}

		var hLines []string
		for k, v := range resp.Header {
			hLines = append(hLines, fmt.Sprintf("%s: %s", headerKeyStyle.Render(k), headerValStyle.Render(strings.Join(v, ", "))))
		}
		if len(hLines) > 0 {
			fmt.Println(headerBlockStyle.Render(strings.Join(hLines, "\n")))
		}

		// Body
		bodyBytes, _ := io.ReadAll(resp.Body)
		var prettyJSON bytes.Buffer
		err = json.Indent(&prettyJSON, bodyBytes, "", "  ")

		if err == nil {
			// Valid JSON -> Draw a nice box
			box := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#FF00FF")).
				Padding(0, 1)

			// Try to use MaxWidth if available to prevent stretching on minified JSON
			// (We just use natural width, prettyJSON usually has line breaks anyway)
			fmt.Println(box.Render(reqDataStyle.Render(prettyJSON.String())))
		} else {
			contentType := strings.ToLower(resp.Header.Get("Content-Type"))
			if strings.Contains(contentType, "text/html") || strings.Contains(strings.ToLower(string(bodyBytes)), "<html") {
				// HTML -> Minimal inline note, NO box!
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true).Render(fmt.Sprintf(" ∅ HTML Response Hidden (%d bytes)", len(bodyBytes))))
			} else {
				// Unknown format -> Just print it with a left border
				dataBlockStyle := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder(), false, false, false, true).
					BorderForeground(lipgloss.Color("#FF00FF")).
					PaddingLeft(1)
				fmt.Println(dataBlockStyle.Render(reqDataStyle.Render(string(bodyBytes))))
			}
		}
	},
}

var grpcCmd = &cobra.Command{
	Use:   "grpc [server:port] [service.Method] [optional-json-body]",
	Short: "Make a gRPC request simply",
	Args:  cobra.RangeArgs(1, 3),
	Run: func(cmd *cobra.Command, args []string) {
		_, err := exec.LookPath("grpcurl")
		if err != nil {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("Error: 'grpcurl' is not installed. Please install it first (e.g., brew install grpcurl)."))
			return
		}

		grpcArgs := []string{"-plaintext"}
		for _, h := range headers {
			grpcArgs = append(grpcArgs, "-H", h)
		}

		server := args[0]
		
		if len(args) == 1 {
			// Auto-list services if only server is provided
			grpcArgs = append(grpcArgs, server, "list")
		} else if len(args) == 2 {
			// Call method without body
			grpcArgs = append(grpcArgs, server, args[1])
		} else if len(args) == 3 {
			// Call method with body
			bodyData := args[2]
			if strings.HasPrefix(bodyData, "@") {
				b, err := os.ReadFile(strings.TrimPrefix(bodyData, "@"))
				if err == nil {
					bodyData = string(b)
				}
			}
			grpcArgs = append(grpcArgs, "-d", bodyData, server, args[1])
		}

		execCmd := exec.Command("grpcurl", grpcArgs...)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		
		fmt.Printf("%s\n\n", reqInfoStyle.Render(fmt.Sprintf("Running: grpcurl %s", strings.Join(grpcArgs, " "))))
		err = execCmd.Run()
		if err != nil {
			fmt.Printf("\ngRPC request failed: %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(httpCmd)
	rootCmd.AddCommand(grpcCmd)

	// HTTP flags
	httpCmd.Flags().StringVarP(&method, "method", "X", "GET", "HTTP Method")
	httpCmd.Flags().StringSliceVarP(&headers, "header", "H", []string{}, "HTTP Header")
	httpCmd.Flags().StringVarP(&data, "data", "d", "", "HTTP Body Data")
	httpCmd.Flags().BoolVarP(&minimal, "minimal", "m", false, "Only show status code and session cookies")

	// GRPC flags
	grpcCmd.Flags().StringSliceVarP(&headers, "header", "H", []string{}, "gRPC Header")
}
