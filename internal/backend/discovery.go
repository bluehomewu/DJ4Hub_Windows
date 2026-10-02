package backend

import "context"

// DiscoveryProvider 裝置發現介面
type DiscoveryProvider interface {
	// ProbeIMEI 透過指定埠探測裝置 IMEI
	// AT 實作：開啟 ttyUSB → AT+GSN
	// QMI 實作：開啟 cdc-wdm → DMS.GetDeviceSerialNumbers
	ProbeIMEI(ctx context.Context, port string) (string, error)
}
