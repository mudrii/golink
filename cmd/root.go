package cmd

import (
	"io"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
)

func newRootCommand(a *app) (*cobra.Command, error) {
	rootCmd := &cobra.Command{
		Use:           rootCommandName,
		Short:         "LinkedIn CLI for humans and agents",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := a.loader.Load()
			if err != nil {
				return a.validationFailure(cmd, "invalid configuration", err.Error())
			}

			a.settings = settings
			a.logger = newLogger(settings.Verbose, a.deps.Stderr)
			if settings.Transport == "unofficial" && !settings.AcceptUnofficialRisk {
				return a.validationFailure(cmd, "missing required flag: --accept-unofficial-risk", "unofficial transport requires explicit acknowledgement in the current implementation")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.SetOut(a.deps.Stdout)
	rootCmd.SetErr(a.deps.Stderr)
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return a.validationFailure(cmd, "invalid flag usage", err.Error())
	})

	flags := rootCmd.PersistentFlags()
	flags.Bool("json", false, "emit strict JSON output")
	flags.Bool("dry-run", false, "preview mutating requests without executing them")
	flags.BoolP("verbose", "v", false, "enable debug logging")
	flags.String("profile", "default", "profile name")
	flags.String("transport", "official", "transport: official|unofficial|auto")
	flags.Bool("accept-unofficial-risk", false, "acknowledge unofficial transport risk")
	flags.Duration("timeout", 30*time.Second, "request timeout")

	if err := a.loader.BindFlags(flags); err != nil {
		return nil, err
	}

	rootCmd.AddCommand(
		newAuthCommand(a),
		newProfileCommand(a),
		newPostCommand(a),
		newCommentCommand(a),
		newReactCommand(a),
		newSearchCommand(a),
		newVersionCommand(a),
	)

	return rootCmd, nil
}

func newLogger(verbose bool, w io.Writer) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
