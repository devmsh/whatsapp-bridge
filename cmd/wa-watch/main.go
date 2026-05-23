// wa-watch — real-time WhatsApp message listener via SSE.
//
// Usage:
//
//	wa-watch [--jid JID] [--host HOST]
//
// Flags:
//
//	--jid   Filter to a specific chat JID (e.g. 972592604155@s.whatsapp.net).
//	        Omit to receive all incoming messages.
//	--host  Bridge base URL (default: http://localhost:8082).
//
// Output:
//
//	One JSON line per message on stdout. Each line is a complete db.Message record.
//	Errors and status lines go to stderr.
//
// Example:
//
//	wa-watch --jid 972592604155@s.whatsapp.net
//	wa-watch | jq '{from: .push_name, text: .content}'
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	jid := flag.String("jid", "", "Filter to chat JID (default: all chats)")
	host := flag.String("host", "http://localhost:8082", "Bridge base URL")
	flag.Parse()

	streamURL := *host + "/api/v2/stream"
	if *jid != "" {
		streamURL += "?jid=" + url.QueryEscape(*jid)
	}

	fmt.Fprintf(os.Stderr, "[wa-watch] connecting to %s\n", streamURL)

	for {
		if err := listen(streamURL); err != nil {
			fmt.Fprintf(os.Stderr, "[wa-watch] disconnected: %v — retrying in 3s\n", err)
			time.Sleep(3 * time.Second)
		}
	}
}

func listen(streamURL string) error {
	resp, err := http.Get(streamURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	fmt.Fprintln(os.Stderr, "[wa-watch] connected — listening for messages")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE comments (e.g. ": connected jid=...") → print to stderr
		if strings.HasPrefix(line, ":") {
			fmt.Fprintln(os.Stderr, "[wa-watch]", strings.TrimPrefix(line, ": "))
			continue
		}

		// SSE data lines → strip "data: " prefix and print JSON to stdout
		if strings.HasPrefix(line, "data: ") {
			json := strings.TrimPrefix(line, "data: ")
			fmt.Println(json)
		}
	}

	return scanner.Err()
}
