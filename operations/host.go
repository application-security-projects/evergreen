package operations

import "github.com/urfave/cli"

func Host() cli.Command {
	return cli.Command{
		Name:  "host",
		Usage: "manage evergreen spawn and build hosts",
		Subcommands: []cli.Command{
			hostCreate(),
			hostModify(),
			hostConfigure(),
			hostStop(),
			hostStart(),
			hostAttach(),
			hostDetach(),
			hostList(),
			hostTerminate(),
			hostProvision(),
			hostSetup(),
			hostSSH(),
			hostRunCommand(),
			hostRsync(),
		},
	}
}
