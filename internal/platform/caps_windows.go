//go:build windows

package platform

func capsInit() Cap {
	return Cap{
		Fallocate:     true,
		MMapAnon:      true,
		InterfaceBind: false,
		UnixSocket:    false,
		Signals:       false,
		Pagesize:      4096,
	}
}
