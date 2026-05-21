//go:build freebsd

package platform

func capsInit() Cap {
	return Cap{
		Fallocate:     true,
		MMapAnon:      true,
		InterfaceBind: true,
		UnixSocket:    true,
		Signals:       true,
		Pagesize:      4096,
	}
}
