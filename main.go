/* Apache v2 license
*  Copyright (C) <2019> Intel Corporation
*
*  SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/edgexfoundry/app-functions-sdk-go/appcontext"
	"github.com/edgexfoundry/app-functions-sdk-go/appsdk"
	"github.com/edgexfoundry/app-functions-sdk-go/pkg/transforms"
	"github.com/edgexfoundry/go-mod-core-contracts/models"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/alert"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/cloudconnector/event"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/config"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/dailyturn"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/heartbeat"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/routes"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/sensor"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/tag"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/app/tagprocessor"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/pkg/jsonrpc"
	"github.com/intel/rsp-sw-toolkit-im-suite-inventory-service/pkg/statemodel"
	"github.com/intel/rsp-sw-toolkit-im-suite-utilities/go-metrics"
	reporter "github.com/intel/rsp-sw-toolkit-im-suite-utilities/go-metrics-influxdb"
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

const (
	serviceKey = "inventory-service"
)

const (
	asnData                  = "ASN_data"
	inventoryData            = "inventory_data"
	deviceAlert              = "device_alert"
	controllerHeartbeat      = "controller_heartbeat"
	sensorConfigNotification = "sensor_config_notification"
	schedulerRunState        = "scheduler_run_state"
	inventoryEvent           = "inventory_event"
	controllerStatusUpdate   = "rsp_controller_status_update"
	controllerReady          = "controller_ready"
)

var (
	// Filter data by value descriptors (aka device resource name)
	valueDescriptors = []string{
		asnData,
		deviceAlert,
		controllerHeartbeat,
		inventoryData,
		sensorConfigNotification,
		schedulerRunState,
		controllerStatusUpdate,
	}
)

type inventoryApp struct {
	masterDB        *sql.DB
	skuMapping      SkuMapping
	edgexSdk        *appsdk.AppFunctionsSDK
	edgexSdkContext *appcontext.Context
	invEventChannel chan *jsonrpc.InventoryEvent
	done            chan bool
}

func newInventoryApp(masterDB *sql.DB) *inventoryApp {
	return &inventoryApp{
		masterDB:        masterDB,
		skuMapping:      NewSkuMapping(config.AppConfig.MappingSkuUrl),
		edgexSdk:        &appsdk.AppFunctionsSDK{ServiceKey: serviceKey},
		invEventChannel: make(chan *jsonrpc.InventoryEvent, 10),
		done:            make(chan bool),
	}
}

func main() {
	mConfigurationError := metrics.GetOrRegisterGauge("Inventory.Main.ConfigurationError", nil)
	mDbConnError := metrics.GetOrRegisterGauge("Inventory.Main.DB-ConnectionError", nil)
	mDbConnection := metrics.GetOrRegisterGauge("Inventory.Main.DB-Connection", nil)

	// Ensure simple text format
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	// Load config variables
	err := config.InitConfig()
	fatalErrorHandler("unable to load configuration variables", err, &mConfigurationError)

	// Initialize metrics reporting
	initMetrics()

	setLoggingLevel(config.AppConfig.LoggingLevel)

	log.WithFields(log.Fields{
		"Method": "main",
		"Action": "Start",
	}).Info("Starting inventory service...")

	// Connection to POSTGRES database
	log.WithFields(log.Fields{"Method": "main", "Action": "Start"}).Info("Connecting to database...")

	db, err := dbSetup(config.AppConfig.DbHost,
		config.AppConfig.DbPort,
		config.AppConfig.DbUser, config.AppConfig.DbPass,
		config.AppConfig.DbName,
		config.AppConfig.DbSSLMode,
	)
	if err != nil {
		mDbConnError.Update(1)
		log.WithFields(log.Fields{
			"Method":  "main",
			"Action":  "Start database",
			"Message": err.Error(),
		}).Fatal("Unable to connect to database.")
	}
	defer db.Close()
	mDbConnection.Update(1)

	// Verify Intel Architecture(IA) when using Probabilistic Algorithm plugin
	if config.AppConfig.ProbabilisticAlgorithmPlugin {
		verifyProbabilisticPlugin()
	}

	invApp := newInventoryApp(db)

	// Connect to EdgeX zeroMQ bus
	go invApp.receiveZMQEvents()

	go invApp.processScheduledTasks()

	go invApp.processInventoryEventChannel()

	go sensor.QueryBasicInfoAllSensors(db)

	// Initiate webserver and routes
	// NOTE: The call to `startWebServer` will block the main thread forever until an osSignal interrupt is received
	startWebServer(db, config.AppConfig.Port, config.AppConfig.ResponseLimit, config.AppConfig.ServiceName)

	log.WithField("Method", "main").Info("Completed.")

}

func startWebServer(masterDB *sql.DB, port string, responseLimit int, serviceName string) {

	// Start Webserver and pass additional data
	router := routes.NewRouter(masterDB, responseLimit)

	// Create a new server and set timeout values.
	server := http.Server{
		Addr:           ":" + port,
		Handler:        router,
		ReadTimeout:    900 * time.Second,
		WriteTimeout:   900 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// We want to report the listener is closed.
	var wg sync.WaitGroup
	wg.Add(1)

	// Start the listener.
	go func() {
		log.Infof("%s running!", serviceName)
		log.Infof("Listener closed : %v", server.ListenAndServe())
		wg.Done()
	}()

	// Listen for an interrupt signal from the OS.
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt)

	// Wait for a signal to shutdown.
	<-osSignals

	// Create a context to attempt a graceful 5 second shutdown.
	const timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Attempt the graceful shutdown by closing the listener and
	// completing all inflight requests.
	if err := server.Shutdown(ctx); err != nil {

		log.WithFields(log.Fields{
			"Method":  "main",
			"Action":  "shutdown",
			"Timeout": timeout,
			"Message": err.Error(),
		}).Error("Graceful shutdown did not complete")

		// Looks like we timedout on the graceful shutdown. Kill it hard.
		if err := server.Close(); err != nil {
			log.WithFields(log.Fields{
				"Method":  "main",
				"Action":  "shutdown",
				"Message": err.Error(),
			}).Error("Error killing server")
		}
	}

	// Wait for the listener to report it is closed.
	wg.Wait()
}

// processShippingNotice processes the list of epcs (shipping notice).  If the epc does not exist in the DB
// an entry is created with a default facility config.AppConfig.AdvancedShippingNoticeFacilityID
// and epc context of the designated value to identify it as a shipping notice
// config.AppConfig.AdvancedShippingNotice.  If the epc does exist, then only epc context value is updated
// with config.AppConfig.AdvancedShippingNotice
func processShippingNotice(data []byte, masterDB *sql.DB, tagsGauge *metrics.GaugeCollection) error {

	var incomingDataSlice []tag.AdvanceShippingNotice
	decoder := json.NewDecoder(bytes.NewBuffer(data))
	if err := decoder.Decode(&incomingDataSlice); err != nil {
		return errors.Wrap(err, "unable to Decode data")
	}

	// do this before inserting the data into the database
	dailyturn.ProcessIncomingASNList(masterDB, incomingDataSlice)

	var tagData []tag.Tag

	for _, asn := range incomingDataSlice {
		if asn.ID == "" || asn.EventTime == "" || asn.SiteID == "" || asn.Items == nil {
			return errors.New("ASN is missing data")
		}
		if tagsGauge != nil {
			(*tagsGauge).Add(int64(len(asn.Items)))
		}

		for _, asnItem := range asn.Items {
			for _, asnEpc := range asnItem.EPCs {
				// create a temporary tag so we can check if it's whitelisted
				tempTag := tag.Tag{}
				tempTag.Epc = asnEpc
				tempTag.ProductID, tempTag.URI, _ = tag.DecodeTagData(asnEpc)
				// TODO: why aren't we checking for invalid tag encodings?

				if len(config.AppConfig.EpcFilters) > 0 {
					// ignore tags that don't match our filters
					if !statemodel.IsTagWhitelisted(tempTag.Epc, config.AppConfig.EpcFilters) {
						continue
					}
				}

				// marshal the ASNContext
				asnContextBytes, err := json.Marshal(tag.ASNContext{
					ASNID:     asn.ID,
					EventTime: asn.EventTime,
					SiteID:    asn.SiteID,
					ItemGTIN:  asnItem.ItemGTIN,
					ItemID:    asnItem.ItemID,
				})
				if err != nil {
					return errors.Wrap(err, "Unable to marshal ASNContext")
				}

				// If the tag exists, update it with the new EPCContext.
				// If it is new, insert it with default FacilityID
				// Note: If bottlenecks may need to redesign to eliminate large number
				// of queries to DB currently this will make a call to the DB PER tag
				tagFromDB, err := tag.FindByEpc(masterDB, tempTag.Epc)
				if err != nil {
					if dbErr := errors.Wrap(err, "Error retrieving tag from database"); dbErr != nil {
						log.Debug(dbErr)
					}
				} else {
					if tagFromDB.IsEmpty() {
						// Tag is not in database, add with defaults
						tempTag.FacilityID = config.AppConfig.AdvancedShippingNoticeFacilityID
						tempTag.EpcContext = string(asnContextBytes)
						tagData = append(tagData, tempTag)
					} else {
						// Found tag, only update the epc context
						tagFromDB.EpcContext = string(asnContextBytes)
						tagData = append(tagData, tagFromDB)
					}
				}
			}
		}
		if len(tagData) > 0 {
			if err := tag.Replace(masterDB, tagData); err != nil {
				return errors.Wrap(err, "error replacing tags")
			}
		}
	}

	return nil
}

func callDeleteTagCollection(masterDB *sql.DB) error {
	log.Debug("received request to delete tag db collection...")
	return tag.DeleteTagCollection(masterDB)
}

// POC only implementation
func markDepartedIfUnseen(tag *jsonrpc.TagEvent, ageOuts map[string]int, currentTimeMillis int64) {
	if tag.EventType == "cycle_count" {
		if minutes, ok := ageOuts[tag.FacilityID]; ok {
			if tag.Timestamp+int64(minutes*60*1000) <= currentTimeMillis {
				tag.EventType = "departed"
			}
		}
	}
}

func initMetrics() {
	// setup metrics reporting
	if config.AppConfig.TelemetryEndpoint != "" {
		go reporter.InfluxDBWithTags(
			metrics.DefaultRegistry,
			time.Second*10, //cfg.ReportingInterval,
			config.AppConfig.TelemetryEndpoint,
			config.AppConfig.TelemetryDataStoreName,
			"",
			"",
			nil,
		)
	}
}

func (invApp *inventoryApp) receiveZMQEvents() {
	//Initialized EdgeX apps functionSDK
	if err := invApp.edgexSdk.Initialize(); err != nil {
		logrus.Errorf("SDK initialization failed: %v", err)
		os.Exit(-1)
	}

	_ = invApp.edgexSdk.SetFunctionsPipeline(
		invApp.contextGrabber,
		transforms.NewFilter(valueDescriptors).FilterByValueDescriptor,
		invApp.processEvents,
	)

	err := invApp.edgexSdk.MakeItRun()
	if err != nil {
		logrus.Errorf("MakeItRun returned error: %v", err)
		os.Exit(-1)
	}
}

// contextGrabber does what it sounds like, it grabs the app-functions-sdk's appcontext.Context. This is needed
// because the context is not available outside of a pipeline without using reflection and unsafe pointers
func (invApp *inventoryApp) contextGrabber(edgexcontext *appcontext.Context, params ...interface{}) (bool, interface{}) {
	if invApp.edgexSdkContext == nil {
		invApp.edgexSdkContext = edgexcontext
		logrus.Debug("grabbed app-functions-sdk context")
	}

	if len(params) < 1 {
		return false, errors.New("no event received")
	}

	existingEvent, ok := params[0].(models.Event)
	if !ok {
		return false, errors.New("type received is not an Event")
	}

	return true, existingEvent
}

func (invApp *inventoryApp) processEvents(edgexcontext *appcontext.Context, params ...interface{}) (bool, interface{}) {
	if len(params) < 1 {
		return false, errors.New("no event received")
	}

	event, ok := params[0].(models.Event)
	if !ok {
		return false, errors.New("type received is not an Event")
	}
	if len(event.Readings) < 1 {
		return false, errors.New("event contains no Readings")
	}

	mRRSHeartbeatReceived := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSHeartbeatReceived", nil)
	mRRSHeartbeatProcessingError := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSHeartbeatError", nil)
	mRRSRawDataProcessingError := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSInventoryDataError", nil)
	mRRSEventsProcessingError := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSEventsError", nil)
	mRRSAlertError := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSAlertError", nil)
	mRRSResetEventReceived := metrics.GetOrRegisterGaugeCollection("Inventory.receiveZMQEvents.RRSResetEventReceived", nil)
	mRRSASNEpcs := metrics.GetOrRegisterGaugeCollection("Inventory.processShippingNotice.RRSASNEpcs", nil)

	for _, reading := range event.Readings {
		switch reading.Name {

		case asnData:
			data, err := base64.StdEncoding.DecodeString(reading.Value)
			if err != nil {
				log.WithFields(log.Fields{
					"Method": "receiveZMQEvents",
					"Action": "ASN data ingestion",
					"Error":  err.Error(),
				}).Error("error decoding base64 value")
				return false, err
			}

			logrus.Debugf("ASN data received: %s", string(data))

			if err := processShippingNotice(data, invApp.masterDB, &mRRSASNEpcs); err != nil {
				log.WithFields(log.Fields{
					"Method": "processShippingNotice",
					"Action": "ASN data ingestion",
					"Error":  err.Error(),
				}).Error("error processing ASN data")
				return false, err
			}
			mRRSASNEpcs.Add(1)

		case controllerHeartbeat:
			mRRSHeartbeatReceived.Update(1)

			logrus.Debugf("Received Heartbeat:\n%s", reading.Value)

			hb := new(jsonrpc.Heartbeat)
			if err := jsonrpc.Decode(reading.Value, hb, &mRRSHeartbeatProcessingError); err != nil {
				return false, err
			}

			if err := heartbeat.ProcessHeartbeat(hb, invApp.masterDB); err != nil {
				errorHandler("error processing heartbeat data", err, &mRRSHeartbeatProcessingError)
				return false, err
			}

		case sensorConfigNotification:
			log.Debugf("Received sensor config notification:\n%s", reading.Value)

			notification := new(jsonrpc.SensorConfigNotification)
			if err := jsonrpc.Decode(reading.Value, notification, nil); err != nil {
				return false, err
			}

			rsp := sensor.NewRSPFromConfigNotification(notification)
			err := sensor.Upsert(invApp.masterDB, rsp)
			if err != nil {
				return false, errors.Wrapf(err, "unable to upsert sensor config notification for sensor %s", notification.Params.DeviceId)
			}

		case schedulerRunState:
			log.Debugf("Received scheduler run state notification:\n%s", reading.Value)

			runState := new(jsonrpc.SchedulerRunState)
			if err := jsonrpc.Decode(reading.Value, runState, nil); err != nil {
				return false, err
			}

			tagprocessor.OnSchedulerRunState(runState)

		case inventoryData:
			log.Debugf("Received inventory_data message. msglen=%d", len(reading.Value))

			invData := new(jsonrpc.InventoryData)
			if err := jsonrpc.Decode(reading.Value, invData, &mRRSRawDataProcessingError); err != nil {
				log.Warn(reading.Value)
				return false, err
			}

			invEvent, err := tagprocessor.ProcessInventoryData(invApp.masterDB, invData)
			if err != nil {
				return false, err
			}
			invApp.invEventChannel <- invEvent

		case deviceAlert:
			log.Debugf("Received device alert data:\n%s", reading.Value)

			rrsAlert, err := alert.ProcessAlert(&reading)
			if err != nil {
				errorHandler("error processing device alert data", err, &mRRSAlertError)
				return false, err
			}

			if rrsAlert.IsInventoryUnloadAlert() {
				mRRSResetEventReceived.Add(1)
				go func(errorGauge *metrics.Gauge) {
					err := callDeleteTagCollection(invApp.masterDB)
					if err != nil {
						errorHandler("error calling delete tag collection", err, errorGauge)
						return
					}

					alertMessage := new(alert.MessagePayload)
					if err := alertMessage.SendDeleteTagCompletionAlertMessage(); err != nil {
						errorHandler("error sending alert message for delete tag collection", err, errorGauge)
					}
				}(&mRRSEventsProcessingError)
			}

		case controllerStatusUpdate:
			log.Debugf("Received controller status update:\n%s", reading.Value)

			notification := new(jsonrpc.ControllerStatusUpdate)
			if err := jsonrpc.Decode(reading.Value, notification, nil); err != nil {
				return false, err
			}

			if notification.Params.Status == controllerReady {
				logrus.Info("rsp controller has been started, querying for all sensor basic info")
				go sensor.QueryBasicInfoAllSensors(invApp.masterDB)
			}
		}
	}

	return false, nil
}

func (invApp *inventoryApp) processInventoryEventChannel() {
	mRRSEventsProcessingError := metrics.GetOrRegisterGauge("Inventory.receiveZMQEvents.RRSEventsError", nil)

	for {
		select {
		case <-invApp.done:
			log.Info("done called. stopping inventory event channel processing")
			close(invApp.invEventChannel)
			return

		case invEvent := <-invApp.invEventChannel:
			if invEvent != nil && !invEvent.IsEmpty() {
				err := invApp.skuMapping.processTagData(invApp, invEvent, "fixed", nil)
				if err != nil {
					errorHandler("error processing event data", err, &mRRSEventsProcessingError)
				}
			}
		}
	}
}

// processScheduledTasks is an infinite loop that processes timer tickers which are basically
// a way to run code on a scheduled interval in golang
func (invApp *inventoryApp) processScheduledTasks() {
	aggregateDepartedTicker := time.NewTicker(time.Duration(config.AppConfig.AggregateDepartedThresholdMillis/5) * time.Millisecond)
	ageoutTicker := time.NewTicker(1 * time.Hour)

	for {
		select {
		case <-invApp.done:
			log.Info("done called. stopping scheduled tasks")
			aggregateDepartedTicker.Stop()
			ageoutTicker.Stop()
			return

		case t := <-aggregateDepartedTicker.C:
			log.Debugf("DoAggregateDepartedTask: %v", t)
			invEvent := tagprocessor.DoAggregateDepartedTask()
			// ingest tag events
			invApp.invEventChannel <- invEvent

		case t := <-ageoutTicker.C:
			log.Debugf("DoAgeoutTask: %v", t)
			tagprocessor.DoAgeoutTask()
		}
	}
}

func (invApp *inventoryApp) pushEventsToCoreData(sentOn int64, controllerId string, tagEvents []tag.Tag) {
	if len(tagEvents) > 0 {
		log.Debugf("%+v", tagEvents)

		payload, err := json.Marshal(event.DataPayload{
			SentOn:             sentOn,
			ControllerId:       controllerId,
			EventSegmentNumber: 1,
			TotalEventSegments: 1,
			TagEvent:           tagEvents,
		})
		if err != nil {
			log.WithFields(log.Fields{
				"Method": "pushEventsToCoreData",
				"Action": "Publish Events to Core Data",
				"Error":  fmt.Sprintf("%+v", err),
			}).Error(err)
			return
		}

		if invApp.edgexSdkContext == nil {
			log.Error("unable to push event to core data due to app-functions-sdk context has not been grabbed yet")
			return
		}
		if _, err = invApp.edgexSdkContext.PushToCoreData(controllerId, inventoryEvent, string(payload)); err != nil {
			log.Errorf("unable to push inventory event to core-data: %v", err)
		}
	}
}

func dbSetup(host, port, user, password, dbname, sslmode string) (*sql.DB, error) {

	// Connect to PostgreSQL database
	psqlConfig := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s",
		host, port, user, dbname, sslmode)
	if password != "" {
		psqlConfig += " password=" + password
	}
	db, err := sql.Open("postgres", psqlConfig)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	log.Info("Connected to postgreSQL database...")

	// Create tables and indexes
	_, errExec := db.Exec(config.DbSchema)
	if errExec != nil {
		return nil, errExec
	}

	return db, nil
}
