//go:build openbsd

package platform

func capsInit() Cap {
	return Cap{
		Fallocate:     false,
		MMapAnon:      true,
		InterfaceBind: true,
		UnixSocket:    true,
		Signals:       true,
		Pagesize:      4096,
	}
}
