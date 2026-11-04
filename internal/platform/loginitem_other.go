//go:build !darwin

package platform

// AppService is SMAppService, which exists only on macOS.
type AppService struct{}

// NewAppService returns a login item that is always unsupported here.
func NewAppService(func() bool) *AppService { return &AppService{} }

func (*AppService) Status() LoginItemStatus { return LoginUnsupported }
func (*AppService) Set(bool) error          { return ErrLoginUnsupported }
