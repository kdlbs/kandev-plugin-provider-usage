// Command server is the backend for the kandev-provider-usage plugin. It shells
// out to the codexbar CLI to report subscription utilization (how much of each
// rate-limit window is used, with reset times) for the user's agent providers:
//
//   - the "providers" webhook lists utilization across every configured provider
//     (rendered by the plugin's Settings page), and
//   - the "session" webhook reports utilization for the provider that backs the
//     current chat session (rendered by the chat-bar icon), resolving the
//     session -> agent -> provider mapping server-side via the Host data API.
//
// It imports only the public pkg/pluginsdk surface — exactly what a third-party
// plugin author would.
package main

import "github.com/kandev/kandev/pkg/pluginsdk"

func main() {
	pluginsdk.Serve(newPlugin())
}
