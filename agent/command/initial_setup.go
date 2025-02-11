package command

import (
	"context"
	"time"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/agent/internal"
	"github.com/evergreen-ci/evergreen/agent/internal/client"
	"github.com/mongodb/jasper"
)

type initialSetup struct{}

func initialSetupFactory() Command                                    { return &initialSetup{} }
func (*initialSetup) Type() string                                    { return evergreen.CommandTypeSystem }
func (*initialSetup) SetType(s string)                                {}
func (*initialSetup) DisplayName() string                             { return "initial task setup" }
func (*initialSetup) SetDisplayName(s string)                         {}
func (*initialSetup) Name() string                                    { return "setup.initial" }
func (*initialSetup) SetIdleTimeout(d time.Duration)                  {}
func (*initialSetup) IdleTimeout() time.Duration                      { return 0 }
func (*initialSetup) ParseParams(params map[string]interface{}) error { return nil }
func (*initialSetup) JasperManager() jasper.Manager                   { return nil }
func (*initialSetup) SetJasperManager(_ jasper.Manager)               {}
func (*initialSetup) Execute(ctx context.Context,
	client client.Communicator, logger client.LoggerProducer, conf *internal.TaskConfig) error {

	logger.Task().Info("performing initial task setup")
	return nil
}
