package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cldmnky/mcp-servers/pkg/mcp/rh-issues-mcp/server"
)

var (
	httpAddr = flag.String("http", "", "HTTP address to listen on (e.g., :8080)")
	help     = flag.Bool("help", false, "Show help message")
)

// setupLogging configures logging to write to a file in the same directory as the binary
func setupLogging(stdioMode bool) (*os.File, error) {
	// Get the directory where the binary is located
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	binDir := filepath.Dir(execPath)

	// Create log file with timestamp
	timestamp := time.Now().Format("20060102")
	logPath := filepath.Join(binDir, fmt.Sprintf("rh-issues-mcp-%s.log", timestamp))

	// Open log file (append mode)
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// In stdio mode, log only to file (stdout/stderr must be clean for MCP protocol)
	// In HTTP mode, log to both file and stderr
	if stdioMode {
		log.SetOutput(logFile)
	} else {
		multiWriter := io.MultiWriter(os.Stderr, logFile)
		log.SetOutput(multiWriter)
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("=== Red Hat JIRA Issues MCP Server Started ===")
	log.Printf("Log file: %s", logPath)

	return logFile, nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Red Hat JIRA Issues MCP Server\n")
		fmt.Fprintf(os.Stderr, "This MCP server provides tools to interact with Red Hat JIRA API:\n")
		fmt.Fprintf(os.Stderr, "1. Search JIRA Issues using JQL\n")
		fmt.Fprintf(os.Stderr, "2. Get Issue Details by Key\n\n")
		fmt.Fprintf(os.Stderr, "Environment Variables:\n")
		fmt.Fprintf(os.Stderr, "  RH_JIRA_EMAIL - Atlassian account email (required)\n")
		fmt.Fprintf(os.Stderr, "  RH_JIRA_TOKEN - Atlassian API token (required)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Run over stdio:     %s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Run HTTP server:    %s -http :8081\n", os.Args[0])
	}
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	// Determine if we're running in stdio mode
	stdioMode := *httpAddr == ""

	// Setup logging
	logFile, err := setupLogging(stdioMode)
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	defer logFile.Close()

	// Check for required environment variable
	if os.Getenv("RH_JIRA_TOKEN") == "" {
		log.Fatal("RH_JIRA_TOKEN environment variable is required")
	}
	if os.Getenv("RH_JIRA_EMAIL") == "" {
		log.Fatal("RH_JIRA_EMAIL environment variable is required")
	}
	log.Println("Jira Cloud credentials found")

	// Create the MCP server
	mcpServer, err := server.NewRedHatIssuesServer()
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}
	log.Println("Red Hat JIRA Issues MCP server initialized successfully")

	ctx := context.Background()

	if *httpAddr != "" {
		// Run as HTTP server
		log.Printf("Starting HTTP server mode on %s", *httpAddr)
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return mcpServer
		}, nil)

		log.Printf("Red Hat JIRA Issues MCP server listening on %s", *httpAddr)
		log.Println("Available tools:")
		log.Println("  - search_issues: Search for Red Hat JIRA issues using JQL")
		log.Println("  - get_issue: Get detailed information about a specific JIRA issue")

		if err := http.ListenAndServe(*httpAddr, handler); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	} else {
		// Run over stdio
		log.Println("Starting stdio mode...")
		log.Println("Available tools:")
		log.Println("  - search_issues: Search for Red Hat JIRA issues using JQL")
		log.Println("  - get_issue: Get detailed information about a specific JIRA issue")

		if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}

	log.Println("=== Red Hat JIRA Issues MCP Server Stopped ===")
}
