// ghost-driver: a browser automation MCP server built on raw CDP.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"

	mcpserver "github.com/Daybeam/ghost-driver/internal/mcp"
)

func main() {
	transport := flag.String("transport", "stdio", "Transport: stdio or sse")
	port := flag.Int("port", 8080, "SSE server port")

	// CLI mode flags
	action := flag.String("action", "", "CLI Action: navigate, click, type, screenshot, get_text, get_markdown, get_html, page_info, smart_click, scroll, hover, navigate_back, navigate_forward, reload, wait_smart, list_tabs, eval")
	headlessFlag := flag.Bool("headless", false, "Run browser in headless mode")
	urlFlag := flag.String("url", "", "URL for navigate")
	xFlag := flag.Float64("x", 0, "X coordinate for click/hover/scroll")
	yFlag := flag.Float64("y", 0, "Y coordinate for click/hover/scroll")
	textFlag := flag.String("text", "", "Text for type")
	jsFlag := flag.String("js", "", "JS expression for eval")
	outFlag := flag.String("out", "", "Output path for screenshot")
	selectorFlag := flag.String("selector", "", "CSS selector for get_markdown/get_html")
	filesFlag := flag.String("files", "", "Semicolon-separated local file paths for upload_files")
	descFlag := flag.String("description", "", "Element description for smart_click")
	selRawFlag := flag.String("selectors", "", "Comma-separated CSS selectors for smart_click")
	dxFlag := flag.Float64("dx", 0, "Scroll delta X")
	dyFlag := flag.Float64("dy", 0, "Scroll delta Y")
	browserPathFlag := flag.String("browser-path", "", "Path to browser binary (chrome/firefox/electron)")
	devPortFlag := flag.Int("dev-port", 9222, "DevTools debugging port")
	versionFlag := flag.Bool("version", false, "Print version and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Ghost-Driver: A browser automation MCP server and CLI.\n\n")
		fmt.Fprintf(os.Stderr, "Usage (MCP mode):\n  ghost-driver --transport stdio\n  ghost-driver --transport sse --port 8080\n\n")
		fmt.Fprintf(os.Stderr, "Usage (CLI mode):\n  ghost-driver --action navigate --url https://google.com\n  ghost-driver --action screenshot --out debug.png --headless=false\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *versionFlag {
		fmt.Println("ghost-driver v1.1.0")
		return
	}

	// 1. IO Protection: In stdio mode, ensure only JSON-RPC hits stdout.
	if *transport == "stdio" {
		log.SetOutput(os.Stderr)
	}

	if *action != "" {
		s := mcpserver.NewState()
		defer s.Close()
		args := map[string]any{
			"url":          *urlFlag,
			"x":            *xFlag,
			"y":            *yFlag,
			"text":         *textFlag,
			"js":           *jsFlag,
			"out":          *outFlag,
			"headless":     *headlessFlag,
			"selector":     *selectorFlag,
			"files":        *filesFlag,
			"description":  *descFlag,
			"selectors":    *selRawFlag,
			"dx":           *dxFlag,
			"dy":           *dyFlag,
			"browser_path": *browserPathFlag,
			"dev_port":     *devPortFlag,
		}
		res, err := s.InvokeCLI(context.Background(), *action, args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(res)
		return
	}

	srv, state := mcpserver.NewServer()
	// Release the audit log file handle when the server loop returns.
	defer state.Close()

	switch *transport {
	case "stdio":
		log.Println("ghost-driver: running on stdio")
		if err := server.ServeStdio(srv); err != nil {
			fmt.Fprintf(os.Stderr, "stdio error: %v\n", err)
			os.Exit(1)
		}
	case "sse":
		addr := fmt.Sprintf(":%d", *port)
		// For backward compatibility with existing client configs, we override the default path (/mcp) to /sse.
		log.Printf("ghost-driver: Streamable HTTP server on http://localhost%s/sse", addr)
		sseSrv := server.NewStreamableHTTPServer(srv, server.WithEndpointPath("/sse"))
		if err := sseSrv.Start(addr); err != nil {
			fmt.Fprintf(os.Stderr, "streamable-http error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown transport %q\n", *transport)
		os.Exit(1)
	}
}
