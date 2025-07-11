// Code generated by MockGen. DO NOT EDIT.
// Source: os.go
//
// Generated by this command:
//
//	mockgen -source=os.go -destination=mock_os.go -package=os
//

// Package os is a generated GoMock package.
package os

import (
	context "context"
	reflect "reflect"

	v1alpha1 "github.com/flightctl/flightctl/api/v1alpha1"
	dependency "github.com/flightctl/flightctl/internal/agent/device/dependency"
	status "github.com/flightctl/flightctl/internal/agent/device/status"
	gomock "go.uber.org/mock/gomock"
)

// MockClient is a mock of Client interface.
type MockClient struct {
	ctrl     *gomock.Controller
	recorder *MockClientMockRecorder
}

// MockClientMockRecorder is the mock recorder for MockClient.
type MockClientMockRecorder struct {
	mock *MockClient
}

// NewMockClient creates a new mock instance.
func NewMockClient(ctrl *gomock.Controller) *MockClient {
	mock := &MockClient{ctrl: ctrl}
	mock.recorder = &MockClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockClient) EXPECT() *MockClientMockRecorder {
	return m.recorder
}

// Apply mocks base method.
func (m *MockClient) Apply(ctx context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Apply", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

// Apply indicates an expected call of Apply.
func (mr *MockClientMockRecorder) Apply(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Apply", reflect.TypeOf((*MockClient)(nil).Apply), ctx)
}

// Status mocks base method.
func (m *MockClient) Status(ctx context.Context) (*Status, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Status", ctx)
	ret0, _ := ret[0].(*Status)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Status indicates an expected call of Status.
func (mr *MockClientMockRecorder) Status(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockClient)(nil).Status), ctx)
}

// Switch mocks base method.
func (m *MockClient) Switch(ctx context.Context, image string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Switch", ctx, image)
	ret0, _ := ret[0].(error)
	return ret0
}

// Switch indicates an expected call of Switch.
func (mr *MockClientMockRecorder) Switch(ctx, image any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Switch", reflect.TypeOf((*MockClient)(nil).Switch), ctx, image)
}

// MockManager is a mock of Manager interface.
type MockManager struct {
	ctrl     *gomock.Controller
	recorder *MockManagerMockRecorder
}

// MockManagerMockRecorder is the mock recorder for MockManager.
type MockManagerMockRecorder struct {
	mock *MockManager
}

// NewMockManager creates a new mock instance.
func NewMockManager(ctrl *gomock.Controller) *MockManager {
	mock := &MockManager{ctrl: ctrl}
	mock.recorder = &MockManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockManager) EXPECT() *MockManagerMockRecorder {
	return m.recorder
}

// AfterUpdate mocks base method.
func (m *MockManager) AfterUpdate(ctx context.Context, desired *v1alpha1.DeviceSpec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AfterUpdate", ctx, desired)
	ret0, _ := ret[0].(error)
	return ret0
}

// AfterUpdate indicates an expected call of AfterUpdate.
func (mr *MockManagerMockRecorder) AfterUpdate(ctx, desired any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AfterUpdate", reflect.TypeOf((*MockManager)(nil).AfterUpdate), ctx, desired)
}

// BeforeUpdate mocks base method.
func (m *MockManager) BeforeUpdate(ctx context.Context, current, desired *v1alpha1.DeviceSpec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BeforeUpdate", ctx, current, desired)
	ret0, _ := ret[0].(error)
	return ret0
}

// BeforeUpdate indicates an expected call of BeforeUpdate.
func (mr *MockManagerMockRecorder) BeforeUpdate(ctx, current, desired any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BeforeUpdate", reflect.TypeOf((*MockManager)(nil).BeforeUpdate), ctx, current, desired)
}

// CollectOCITargets mocks base method.
func (m *MockManager) CollectOCITargets(ctx context.Context, current, desired *v1alpha1.DeviceSpec) ([]dependency.OCIPullTarget, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CollectOCITargets", ctx, current, desired)
	ret0, _ := ret[0].([]dependency.OCIPullTarget)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CollectOCITargets indicates an expected call of CollectOCITargets.
func (mr *MockManagerMockRecorder) CollectOCITargets(ctx, current, desired any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CollectOCITargets", reflect.TypeOf((*MockManager)(nil).CollectOCITargets), ctx, current, desired)
}

// Reboot mocks base method.
func (m *MockManager) Reboot(ctx context.Context, desired *v1alpha1.DeviceSpec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Reboot", ctx, desired)
	ret0, _ := ret[0].(error)
	return ret0
}

// Reboot indicates an expected call of Reboot.
func (mr *MockManagerMockRecorder) Reboot(ctx, desired any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Reboot", reflect.TypeOf((*MockManager)(nil).Reboot), ctx, desired)
}

// Status mocks base method.
func (m *MockManager) Status(arg0 context.Context, arg1 *v1alpha1.DeviceStatus, arg2 ...status.CollectorOpt) error {
	m.ctrl.T.Helper()
	varargs := []any{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "Status", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// Status indicates an expected call of Status.
func (mr *MockManagerMockRecorder) Status(arg0, arg1 any, arg2 ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Status", reflect.TypeOf((*MockManager)(nil).Status), varargs...)
}
