package wallet

import (
    "fmt"
    "os"

    "github.com/urfave/cli"

    cliutils "github.com/rocket-pool/smartnode/shared/utils/cli"
)


// Register commands
func RegisterCommands(app *cli.App, name string, aliases []string) {
    app.Commands = append(app.Commands, cli.Command{
        Name:      name,
        Aliases:   aliases,
        Usage:     "Manage the node wallet",
        Subcommands: []cli.Command{

            cli.Command{
                Name:      "status",
                Aliases:   []string{"s"},
                Usage:     "Get the node wallet status",
                UsageText: "rocketpool wallet status",
                Action: func(c *cli.Context) error {

                    // Validate args
                    if err := cliutils.ValidateArgCount(c, 0); err != nil { return err }

                    // Run
                    return getStatus(c)

                },
            },

            cli.Command{
                Name:      "init",
                Aliases:   []string{"i"},
                Usage:     "Initialize the node wallet",
                UsageText: "rocketpool wallet init [options]",
                Flags: []cli.Flag{
                    cli.StringFlag{
                        Name:  "password, p",
                        Usage: "The password to secure the wallet with (if not already set)",
                    },
                    cli.BoolFlag{
                        Name:  "confirm-mnemonic, c",
                        Usage: "Automatically confirm the mnemonic phrase",
                    },
                },
                Action: func(c *cli.Context) error {

                    // Validate args
                    if err := cliutils.ValidateArgCount(c, 0); err != nil { return err }

                    // Validate flags
                    if c.String("password") != "" {
                        if _, err := cliutils.ValidateNodePassword("password", c.String("password")); err != nil { return err }
                    }

                    // Run
                    return initWallet(c)

                },
            },

            cli.Command{
                Name:      "recover",
                Aliases:   []string{"r"},
                Usage:     "Recover a node wallet from a mnemonic phrase",
                UsageText: "rocketpool wallet recover [options]",
                Flags: []cli.Flag{
                    cli.StringFlag{
                        Name:  "password, p",
                        Usage: "The password to secure the wallet with (if not already set)",
                    },
                    cli.StringFlag{
                        Name:  "mnemonic, m",
                        Usage: "The mnemonic phrase to recover the wallet from",
                    },
                },
                Action: func(c *cli.Context) error {

                    // Validate args
                    if err := cliutils.ValidateArgCount(c, 0); err != nil { return err }

                    // Validate flags
                    if c.String("password") != "" {
                        if _, err := cliutils.ValidateNodePassword("password", c.String("password")); err != nil { return err }
                    }
                    if c.String("mnemonic") != "" {
                        if _, err := cliutils.ValidateWalletMnemonic("mnemonic", c.String("mnemonic")); err != nil { return err }
                    }

                    // Run
                    return recoverWallet(c)

                },
            },

            cli.Command{
                Name:      "rebuild",
                Aliases:   []string{"b"},
                Usage:     "Rebuild validator keystores from derived keys",
                UsageText: "rocketpool wallet rebuild",
                Action: func(c *cli.Context) error {

                    // Validate args
                    if err := cliutils.ValidateArgCount(c, 0); err != nil { return err }

                    // Run
                    return rebuildWallet(c)

                },
            },

            cli.Command{
                Name:      "export",
                Aliases:   []string{"e"},
                Usage:     "Export the node wallet in JSON format",
                UsageText: "rocketpool wallet export",
                Flags: []cli.Flag{
                    cli.BoolFlag{
                        Name:  "force, f",
                        Usage: "Skips warnings about printing sensitive information",
                    },
                },
                Action: func(c *cli.Context) error {
                    colorYellow := "\033[33m"
		    colorReset := "\033[0m"

                    // Validate args
                    if err := cliutils.ValidateArgCount(c, 0); err != nil { return err }

                    // Prompt for user confirmation
                    if !c.Bool("force") {
                        stat, err := os.Stdout.Stat()
                        if err != nil {
                            fmt.Fprintf(os.Stderr, "Error checking stdout stat: %w.\nUse --force to export wallet.\n", err)
			    return nil
                        }

                        if (stat.Mode() & os.ModeCharDevice) != os.ModeCharDevice {
                            fmt.Fprintln(os.Stderr, "Call wallet export with --force to export wallet to non-interactive terminal.")
			    return nil
                        }

                        if !cliutils.Confirm(fmt.Sprintf("%sExporting a wallet will print sensitive information to your screen.%s\n" +
                            "Are you sure you want to continue?", colorYellow, colorReset)) {
                                fmt.Println("Cancelled.")
                                return nil
                        }
                    }

                    // Run
                    return exportWallet(c)

                },
            },

        },
    })
}

