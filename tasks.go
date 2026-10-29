package main

import (
	"sync"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	gentleman "gopkg.in/h2non/gentleman.v2"
)

// The generator's before-request hooks are a process-wide registry with no
// way to remove one, so the cursor hook is added once and reads the flag off
// whichever `tasks account-list` was registered last.
var (
	taskListCommand    *cobra.Command
	taskListCursorHook sync.Once
)

// registerAccountTaskListPaging gives `tasks account-list` a --cursor flag. The endpoint is
// cursor-paginated and answers with `meta.next_cursor`, but its spec documents
// no cursor parameter, so the generated command could only ever read the first
// page: past the first 100 tasks, a filtered or searched listing had no way on.
func registerAccountTaskListPaging(root *cobra.Command) {
	list := findSubcommand(root, "tasks", "account-list")
	if list == nil {
		return
	}
	list.Long += "\n\n" + cli.Markdown("Find a task by words in its title with `--q` (two dashes: `-q` is the global `--query` projection). It combines with `--status`, `--owner-user-id`, and the other filters, and with `--cursor`: pass the `meta.next_cursor` of one page back as `--cursor`, with the same filters and `--q`, to fetch the next.")
	list.Flags().String("cursor", "", "Opaque pagination cursor. Pass the meta.next_cursor value from a previous response, with the same filters, to fetch the next page; omit it for the first page.")
	taskListCommand = list
	taskListCursorHook.Do(func() {
		cli.RegisterBefore("listtasks", func(_ string, _ *viper.Viper, req *gentleman.Request) {
			if cursor, _ := taskListCommand.Flags().GetString("cursor"); cursor != "" {
				req.AddQuery("cursor", cursor)
			}
		})
	})
}
