package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cldmnky/mcp-servers/internal/logging"
	"github.com/cldmnky/mcp-servers/internal/setuphelp"
	"github.com/cldmnky/mcp-servers/pkg/mcp/rh-issues-mcp/server"
)

const service = "rh-issues-mcp"

var (
	httpAddr = flag.String("http", "", "HTTP address to listen on (e.g., localhost:8081)")
	logDir   = flag.String("log-dir", "", "directory for the rotated log file (default: beside the binary)")
	verbose  = flag.Bool("v", false, "verbose (debug) logging; also settable via LOG_LEVEL")
	help     = flag.Bool("help", false, "Show help message")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Red Hat JIRA Issues MCP Server\n")
		fmt.Fprintf(os.Stderr, "This MCP server provides tools to interact with Red Hat JIRA API:\n")
		fmt.Fprintf(os.Stderr, "1. Search JIRA Issues using JQL\n")
		fmt.Fprintf(os.Stderr, "2. Get Issue Details by Key\n\n")
		fmt.Fprintf(os.Stderr, "Environment Variables:\n")
		fmt.Fprintf(os.Stderr, "  RH_JIRA_EMAIL - Atlassian account email (required)\n")
		fmt.Fprintf(os.Stderr, "  RH_JIRA_TOKEN - Atlassian API token (required)\n")
		fmt.Fprintf(os.Stderr, "  LOG_LEVEL     - debug, info, warn (default), or error\n")
		setuphelp.Write(os.Stderr, "rh-issues", service, []setuphelp.EnvVar{
			{Name: "RH_JIRA_EMAIL", Placeholder: "you@example.com"},
			{Name: "RH_JIRA_TOKEN", Placeholder: "your_atlassian_api_token"},
		})
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  Run over stdio:     %s\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Run HTTP server:    %s -http localhost:8081\n", os.Args[0])
	}
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	// Logging is rotated (size-based, compressed, old backups pruned) and
	// quiet by default: only warnings and errors unless -v or LOG_LEVEL.
	// stdio mode logs to the file only; HTTP mode mirrors to stderr.
	logPath, closeLog := logging.Init(logging.Options{
		Service: service,
		Dir:     *logDir,
		Verbose: *verbose,
		Stderr:  *httpAddr != "",
	})
	defer closeLog()

	if os.Getenv("RH_JIRA_TOKEN") == "" {
		logging.Errorf("%s: RH_JIRA_TOKEN environment variable is required", service)
		os.Exit(1)
	}
	if os.Getenv("RH_JIRA_EMAIL") == "" {
		logging.Errorf("%s: RH_JIRA_EMAIL environment variable is required", service)
		os.Exit(1)
	}

	mcpServer, err := server.NewRedHatIssuesServer()
	if err != nil {
		logging.Errorf("%s: failed to create server: %v", service, err)
		os.Exit(1)
	}

	ctx := context.Background()

	if *httpAddr != "" {
		if err := serveHTTP(ctx, *httpAddr, mcpServer, logPath); err != nil {
			logging.Errorf("%s: HTTP server failed: %v", service, err)
			os.Exit(1)
		}
		return
	}

	logging.Infof("%s: starting stdio mode (log: %s)", service, logPath)
	if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logging.Errorf("%s: server failed: %v", service, err)
		os.Exit(1)
	}
}

// serveHTTP runs the Streamable HTTP transport with bounded timeouts and
// graceful shutdown on SIGINT/SIGTERM. It returns nil on a clean shutdown.
func serveHTTP(ctx context.Context, addr string, mcpServer *mcp.Server, logPath string) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, nil)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
		// Bound timeouts so slow or idle clients cannot hold connections
		// forever. WriteTimeout must exceed any long-poll window the MCP
		// client uses.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	shutdown := make(chan error, 1)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		sig := <-sigCh
		logging.Infof("%s: %v received, shutting down", service, sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown <- srv.Shutdown(ctx)
	}()

	logging.Infof("%s: listening on http://%s (tools: search_issues, get_issue; log: %s)", service, addr, logPath)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return <-shutdown
	}
	return err
}
