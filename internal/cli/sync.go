package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *app) sync() *cobra.Command {
	return &cobra.Command{Use: "sync", Short: "Get frames from the server and push new ones.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.open()
		if err != nil {
			return err
		}
		pulled, pushed, err := s.Sync(nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Received %d frames from the server\n", pulled)
		fmt.Fprintf(cmd.OutOrStdout(), "Pushed %d frames to the server\n", pushed)
		return nil
	}}
}
