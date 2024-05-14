/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package server

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/openshift-kni/oran-o2ims/internal"
	"github.com/openshift-kni/oran-o2ims/internal/authentication"
	"github.com/openshift-kni/oran-o2ims/internal/authorization"
	"github.com/openshift-kni/oran-o2ims/internal/exit"
	"github.com/openshift-kni/oran-o2ims/internal/logging"
	"github.com/openshift-kni/oran-o2ims/internal/network"
	"github.com/openshift-kni/oran-o2ims/internal/service"
)

// Server creates and returns the `start alarm-notification-server` command.
func AlarmNotificationServer() *cobra.Command {
	c := NewAlarmNotificationServer()
	result := &cobra.Command{
		Use:   "alarm-notification-server",
		Short: "Starts the alarm Notification Server",
		Args:  cobra.NoArgs,
		RunE:  c.run,
	}
	flags := result.Flags()

	authentication.AddFlags(flags)
	authorization.AddFlags(flags)

	network.AddListenerFlags(flags, network.APIListener, network.APIAddress)
	network.AddListenerFlags(flags, network.MetricsListener, network.MetricsAddress)
	_ = flags.String(
		cloudIDFlagName,
		"",
		"O-Cloud identifier.",
	)
	_ = flags.StringArray(
		extensionsFlagName,
		[]string{},
		"Extension to add to alarm notifications.",
	)
	return result
}

// alarmNotificationServerCommand contains the data and logic needed to run the `start
// alarm-notification-server` command.
type AlarmNotificationServerCommand struct {
}

// NewAlarmNotificationServer creates a new runner that knows how to execute the `start
// alarm-notification-server` command.
func NewAlarmNotificationServer() *AlarmNotificationServerCommand {
	return &AlarmNotificationServerCommand{}
}

// run executes the `start alarm-notification-server` command.
func (c *AlarmNotificationServerCommand) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the dependencies from the context:
	logger := internal.LoggerFromContext(ctx)

	// Get the flags:
	flags := cmd.Flags()

	// Get the cloud identifier:
	cloudID, err := flags.GetString(cloudIDFlagName)
	if err != nil {
		logger.Error(
			"Failed to get cloud identifier flag",
			"flag", cloudIDFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	if cloudID == "" {
		logger.Error(
			"Cloud identifier is empty",
			"flag", cloudIDFlagName,
		)
		return exit.Error(1)
	}
	logger.Info(
		"Cloud identifier",
		"value", cloudID,
	)

	// Get the extensions details:
	extensions, err := flags.GetStringArray(extensionsFlagName)
	if err != nil {
		logger.Error(
			"Failed to extension flag",
			"flag", extensionsFlagName,
			"error", err.Error(),
		)
		return exit.Error(1)
	}
	logger.Info(
		"alarm notification extensions details",
		slog.Any("extensions", extensions),
	)

	// Create the logging wrapper:
	loggingWrapper, err := logging.NewTransportWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create transport wrapper",
			"error", err.Error(),
		)
		return exit.Error(1)
	}

	// Create the authentication and authorization wrappers:
	authenticationWrapper, err := authentication.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create authentication wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	authorizationWrapper, err := authorization.NewHandlerWrapper().
		SetLogger(logger).
		SetFlags(flags).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create authorization wrapper",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	// Create the router:
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusNotFound, "Not found")
	})
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.SendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	})
	router.Use(authenticationWrapper, authorizationWrapper)

	// Create the handler:
	handler, err := service.NewAlarmNotificationHandler().
		SetLogger(logger).
		SetLoggingWrapper(loggingWrapper).
		SetCloudID(cloudID).
		SetExtensions(extensions...).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create handler",
			"error", err,
		)
		return exit.Error(1)
	}

	//read DB to recovery
	//When real DB is available, we need hanld recovery errors
	handler.RecoveryFromDb()

	// Create the routes:
	adapter, err := service.NewAdapter().
		SetLogger(logger).
		SetPathVariables("alarmNotificationID").
		SetHandler(handler).
		Build()
	if err != nil {
		logger.Error(
			"Failed to create adapter",
			"error", err,
		)
		return exit.Error(1)
	}
	router.Handle(
		"/o2ims-infrastructureMonitoring/{version}/alarmNotifications",
		adapter,
	).Methods(http.MethodPost)

	// Start the API server:
	apiListener, err := network.NewListener().
		SetLogger(logger).
		SetFlags(flags, network.APIListener).
		Build()
	if err != nil {
		logger.Error(
			"Failed to to create API listener",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}
	logger.Info(
		"API listening",
		slog.String("address", apiListener.Addr().String()),
	)
	apiServer := http.Server{
		Addr:    apiListener.Addr().String(),
		Handler: router,
	}
	err = apiServer.Serve(apiListener)
	if err != nil {
		logger.Error(
			"API server finished with error",
			slog.String("error", err.Error()),
		)
		return exit.Error(1)
	}

	return nil
}
