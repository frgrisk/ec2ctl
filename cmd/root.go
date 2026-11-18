/*
Copyright © 2021 FRG

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/frgrisk/ec2ctl/cmd/types"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var regions []string

var output types.Output

var tags map[string]string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ec2ctl",
	Short: "Your EC2 fleet, at a glance",
	Long: `ec2ctl — your EC2 fleet, at a glance.

Start here: ec2ctl demo
Explore a sample fleet without AWS requests.

Find instances by region, tag or ID. Start, stop, resize or terminate them.
Instance IDs are optional for status, start and stop.
Omit --regions to search all enabled regions.`,
	Example: `  # Find development instances
  ec2ctl status --tag Environment=dev

  # Export fleet data
  ec2ctl status --output json

  # Start stopped development instances
  ec2ctl start --regions us-east-1 --tag Environment=dev`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := rootCmd.ExecuteContext(ctx)
	stop()
	cobra.CheckErr(err)
}

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: exploreGroup, Title: "Explore:"},
		&cobra.Group{ID: changeGroup, Title: "Change instances:"},
		&cobra.Group{ID: toolsGroup, Title: "Shell and help tools:"},
	)
	rootCmd.SetHelpCommandGroupID(toolsGroup)
	rootCmd.SetCompletionCommandGroupID(toolsGroup)
	rootCmd.SetHelpFunc(commandHelp)
	cobra.OnInitialize(initConfig)
	// Global Flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.ec2ctl.yaml)")
	rootCmd.PersistentFlags().StringSliceVar(&regions, "regions", []string{}, "comma-separated AWS regions (default: all enabled regions)")
	rootCmd.PersistentFlags().VarP(&output, "output", "o", "status and demo output format: table or json")
	rootCmd.PersistentFlags().StringToStringVar(&tags, "tag", map[string]string{}, "query by tags - specified as key=value pairs (e.g. Environment=dev,Name=dev.example.com)")

	log.SetReportTimestamp(false)
	log.SetReportCaller(true)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".ec2ctl" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".ec2ctl")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
