// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package management

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/client/mock"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/logp"

	"github.com/elastic/beats/v7/libbeat/common/reload"
	"github.com/elastic/beats/v7/libbeat/features"
)

func TestManagerV2(t *testing.T) {
	r := reload.NewRegistry()

	output := &reloadable{}
	r.MustRegisterOutput(output)
	inputs := &reloadableList{}
	r.MustRegisterInput(inputs)

	configsSet := false
	configsCleared := false
	logLevelSet := false
	fqdnEnabled := false
	allStopped := false
	onObserved := func(observed *proto.CheckinObserved, currentIdx int) {
		if currentIdx == 1 {
			oCfg := output.Config()
			iCfgs := inputs.Configs()
			if oCfg != nil && len(iCfgs) == 3 {
				configsSet = true
				t.Logf("output and inputs configuration set")
			}
		} else if currentIdx == 2 {
			oCfg := output.Config()
			iCfgs := inputs.Configs()
			if oCfg == nil || len(iCfgs) != 3 {
				// should not happen (config no longer set)
				configsSet = false
				t.Logf("output and inputs configuration cleared (should not happen)")
			}
		} else {
			oCfg := output.Config()
			iCfgs := inputs.Configs()
			if oCfg == nil && len(iCfgs) == 0 {
				configsCleared = true
			}
			if len(observed.Units) == 0 {
				allStopped = true
				t.Logf("output and inputs configuration cleared (stopping)")
			}
		}
		if logp.GetLevel() == zapcore.DebugLevel {
			logLevelSet = true
			t.Logf("debug log level set")
		}

		fqdnEnabled = features.FQDN()
		t.Logf("FQDN feature flag set to %v", fqdnEnabled)
	}

	srv := mockSrv([][]*proto.UnitExpected{
		{
			{
				Id:             "output-unit",
				Type:           proto.UnitType_OUTPUT,
				ConfigStateIdx: 1,
				Config: &proto.UnitExpectedConfig{
					Id:   "default",
					Type: "elasticsearch",
					Name: "elasticsearch",
				},
				State:    proto.State_HEALTHY,
				LogLevel: proto.UnitLogLevel_INFO,
			},
			{
				Id:             "input-unit-1",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				Config: &proto.UnitExpectedConfig{
					Id:   "system/metrics-system-default-system-1",
					Type: "system/metrics",
					Name: "system-1",
					Streams: []*proto.Stream{
						{
							Id: "system/metrics-system.filesystem-default-system-1",
							Source: requireNewStruct(t, map[string]interface{}{
								"metricsets": []interface{}{"filesystem"},
								"period":     "1m",
							}),
						},
					},
				},
				State:    proto.State_HEALTHY,
				LogLevel: proto.UnitLogLevel_INFO,
			},
			{
				Id:             "input-unit-2",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				Config: &proto.UnitExpectedConfig{
					Id:   "system/metrics-system-default-system-2",
					Type: "system/metrics",
					Name: "system-2",
					Streams: []*proto.Stream{
						{
							Id: "system/metrics-system.filesystem-default-system-2",
							Source: requireNewStruct(t, map[string]interface{}{
								"metricsets": []interface{}{"filesystem"},
								"period":     "1m",
							}),
						},
						{
							Id: "system/metrics-system.filesystem-default-system-3",
							Source: requireNewStruct(t, map[string]interface{}{
								"metricsets": []interface{}{"filesystem"},
								"period":     "1m",
							}),
						},
					},
				},
				State:    proto.State_HEALTHY,
				LogLevel: proto.UnitLogLevel_INFO,
			},
		},
		{
			{
				Id:             "output-unit",
				Type:           proto.UnitType_OUTPUT,
				ConfigStateIdx: 1,
				State:          proto.State_HEALTHY,
				LogLevel:       proto.UnitLogLevel_INFO,
			},
			{
				Id:             "input-unit-1",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				State:          proto.State_HEALTHY,
				LogLevel:       proto.UnitLogLevel_DEBUG,
			},
			{
				Id:             "input-unit-2",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				State:          proto.State_HEALTHY,
				LogLevel:       proto.UnitLogLevel_INFO,
			},
		},
		{
			{
				Id:             "output-unit",
				Type:           proto.UnitType_OUTPUT,
				ConfigStateIdx: 1,
				State:          proto.State_STOPPED,
				LogLevel:       proto.UnitLogLevel_INFO,
			},
			{
				Id:             "input-unit-1",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				State:          proto.State_STOPPED,
				LogLevel:       proto.UnitLogLevel_DEBUG,
			},
			{
				Id:             "input-unit-2",
				Type:           proto.UnitType_INPUT,
				ConfigStateIdx: 1,
				State:          proto.State_STOPPED,
				LogLevel:       proto.UnitLogLevel_INFO,
			},
		},
		{},
	},
		[]uint64{1, 2, 2, 2},
		[]*proto.Features{
			nil,
			{Fqdn: &proto.FQDNFeature{Enabled: true}},
			nil,
			nil,
		},
		onObserved,
		500*time.Millisecond,
	)
	require.NoError(t, srv.Start())
	defer srv.Stop()

	client := client.NewV2(fmt.Sprintf(":%d", srv.Port), "", client.VersionInfo{
		Name:    "program",
		Version: "v1.0.0",
		Meta: map[string]string{
			"key": "value",
		},
	}, grpc.WithTransportCredentials(insecure.NewCredentials()))

	m, err := NewV2AgentManagerWithClient(&Config{
		Enabled: true,
	}, r, client)
	require.NoError(t, err)

	err = m.Start()
	require.NoError(t, err)
	defer m.Stop()

	require.Eventually(t, func() bool {
		return configsSet && configsCleared && logLevelSet && fqdnEnabled && allStopped
	}, 15*time.Second, 300*time.Millisecond)
}

func TestOutputError(t *testing.T) {
	// Uncomment the line below to see the debug logs for this test
	// logp.DevelopmentSetup(logp.WithLevel(logp.DebugLevel), logp.WithSelectors("*"))
	r := reload.NewRegistry()

	output := &mockOutput{
		ReloadFn: func(config *reload.ConfigWithMeta) error {
			return errors.New("any kind of error will do")
		},
	}
	r.MustRegisterOutput(output)
	inputs := &mockReloadable{
		ReloadFn: func(configs []*reload.ConfigWithMeta) error {
			err := errors.New("Inputs should not be reloaded if the output fails")
			t.Fatal(err)
			return err
		},
	}
	r.MustRegisterInput(inputs)

	stateReached := false
	units := []*proto.UnitExpected{
		{
			Id:             "output-unit",
			Type:           proto.UnitType_OUTPUT,
			State:          proto.State_HEALTHY,
			ConfigStateIdx: 1,
			LogLevel:       proto.UnitLogLevel_DEBUG,
			Config: &proto.UnitExpectedConfig{
				Id:   "default",
				Type: "mock",
				Name: "mock",
				Source: requireNewStruct(t,
					map[string]interface{}{
						"Is":        "this",
						"required?": "Yes!",
					}),
			},
		},
		{
			Id:             "input-unit",
			Type:           proto.UnitType_INPUT,
			State:          proto.State_HEALTHY,
			ConfigStateIdx: 1,
			LogLevel:       proto.UnitLogLevel_DEBUG,
		},
	}

	desiredState := []*proto.UnitExpected{
		{
			Id:             "output-unit",
			Type:           proto.UnitType_OUTPUT,
			State:          proto.State_FAILED,
			ConfigStateIdx: 1,
			LogLevel:       proto.UnitLogLevel_DEBUG,
			Config: &proto.UnitExpectedConfig{
				Id:   "default",
				Type: "mock",
				Name: "mock",
				Source: requireNewStruct(t,
					map[string]interface{}{
						"this":     "is",
						"required": true,
					}),
			},
		},
		{
			Id:             "input-unit",
			Type:           proto.UnitType_INPUT,
			State:          proto.State_STARTING,
			ConfigStateIdx: 1,
			LogLevel:       proto.UnitLogLevel_DEBUG,
		},
	}

	server := &mock.StubServerV2{
		CheckinV2Impl: func(observed *proto.CheckinObserved) *proto.CheckinExpected {
			if DoesStateMatch(observed, desiredState, 0) {
				stateReached = true
			}
			return &proto.CheckinExpected{
				Units: units,
			}
		},
		ActionImpl: func(response *proto.ActionResponse) error { return nil },
	}

	if err := server.Start(); err != nil {
		t.Fatalf("could not start mock Elastic-Agent server: %s", err)
	}
	defer server.Stop()

	client := client.NewV2(
		fmt.Sprintf(":%d", server.Port),
		"",
		client.VersionInfo{},
		grpc.WithTransportCredentials(insecure.NewCredentials()))

	m, err := NewV2AgentManagerWithClient(
		&Config{
			Enabled: true,
		},
		r,
		client,
	)
	if err != nil {
		t.Fatalf("could not instantiate ManagerV2: %s", err)
	}

	mm, ok := m.(*BeatV2Manager)
	if !ok {
		t.Fatalf("unexpected type for BeatV2Manager: %T", m)
	}

	mm.changeDebounce = 10 * time.Millisecond
	mm.forceReloadDebounce = 100 * time.Millisecond

	if err := m.Start(); err != nil {
		t.Fatalf("could not start ManagerV2: %s", err)
	}
	defer m.Stop()

	require.Eventually(t, func() bool {
		return stateReached
	}, 10*time.Second, 100*time.Millisecond, "desired state, output failed, was not reached")
}

func mockSrv(
	units [][]*proto.UnitExpected,
	featuresIdxs []uint64,
	features []*proto.Features,
	observedCallback func(*proto.CheckinObserved, int),
	delay time.Duration,
) *mock.StubServerV2 {
	i := 0
	agentInfo := &proto.CheckinAgentInfo{
		Id:       "elastic-agent-id",
		Version:  "8.6.0",
		Snapshot: true,
	}
	return &mock.StubServerV2{
		CheckinV2Impl: func(observed *proto.CheckinObserved) *proto.CheckinExpected {
			if observedCallback != nil {
				observedCallback(observed, i)
			}
			matches := DoesStateMatch(observed, units[i], featuresIdxs[i])
			if !matches {
				// send same set of units and features
				return &proto.CheckinExpected{
					AgentInfo:   agentInfo,
					Units:       units[i],
					Features:    features[i],
					FeaturesIdx: featuresIdxs[i],
				}
			}
			// delay sending next expected based on delay
			if delay > 0 {
				<-time.After(delay)
			}
			// send next set of units and features
			i += 1
			if i >= len(units) {
				// stay on last index
				i = len(units) - 1
			}
			return &proto.CheckinExpected{
				AgentInfo:   agentInfo,
				Units:       units[i],
				Features:    features[i],
				FeaturesIdx: featuresIdxs[i],
			}
		},
		ActionImpl: func(response *proto.ActionResponse) error {
			// actions not tested here
			return nil
		},
		ActionsChan: make(chan *mock.PerformAction, 100),
	}
}

type reloadable struct {
	mx     sync.Mutex
	config *reload.ConfigWithMeta
}

type reloadableList struct {
	mx      sync.Mutex
	configs []*reload.ConfigWithMeta
}

func (r *reloadable) Reload(config *reload.ConfigWithMeta) error {
	r.mx.Lock()
	defer r.mx.Unlock()
	r.config = config
	return nil
}

func (r *reloadable) Config() *reload.ConfigWithMeta {
	r.mx.Lock()
	defer r.mx.Unlock()
	return r.config
}

func (r *reloadableList) Reload(configs []*reload.ConfigWithMeta) error {
	r.mx.Lock()
	defer r.mx.Unlock()
	r.configs = configs
	return nil
}

func (r *reloadableList) Configs() []*reload.ConfigWithMeta {
	r.mx.Lock()
	defer r.mx.Unlock()
	return r.configs
}

type mockOutput struct {
	mutex     sync.Mutex
	ReloadFn  func(config *reload.ConfigWithMeta) error
	ConfigFn  func() *reload.ConfigWithMeta
	ConfigsFn func() []*reload.ConfigWithMeta
}

func (m *mockOutput) Reload(config *reload.ConfigWithMeta) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ReloadFn(config)
}

func (m *mockOutput) Config() *reload.ConfigWithMeta {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ConfigFn()
}

func (m *mockOutput) Configs() []*reload.ConfigWithMeta {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ConfigsFn()
}

type mockReloadable struct {
	mutex     sync.Mutex
	ReloadFn  func(configs []*reload.ConfigWithMeta) error
	ConfigFn  func() *reload.ConfigWithMeta
	ConfigsFn func() []*reload.ConfigWithMeta
}

func (m *mockReloadable) Reload(configs []*reload.ConfigWithMeta) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ReloadFn(configs)
}

func (m *mockReloadable) Config() *reload.ConfigWithMeta {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ConfigFn()
}

func (m *mockReloadable) Configs() []*reload.ConfigWithMeta {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.ConfigsFn()
}
