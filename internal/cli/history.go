package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/moeritze/harbormaster/internal/app"
)

func newHistory(a *app.App) *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show pruned and released entries",
		RunE: func(_ *cobra.Command, _ []string) error {
			recs, err := a.Store.History(limit)
			if err != nil {
				return exitf(ExitRegistry, "%v", err)
			}
			if asJSON {
				return writeJSON(a.Stdout, recs)
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "AT\tPORT\tREASON\tAGENT\tWORKTREE\tLABEL")
			for _, r := range recs {
				_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", r.At.Local().Format("2006-01-02 15:04 MST"), r.Port, r.Reason, render(r.Agent), shortPath(render(r.Worktree)), orDash(render(r.Label)))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().IntVar(&limit, "limit", 50, "most recent N records, 0 for all")
	return cmd
}
