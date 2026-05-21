//go:build darwin

package platform

func capsInit() Cap {
	return Cap{
		Fallocate:     true,
		MMapAnon:      true,
		InterfaceBind: false,
		UnixSocket:    true,
		Signals:       true,
		Pagesize:      16384,
	}
}
