package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#Host:           #NonEmptyString

#Metadata: {
	name: #NonEmptyString
}

#ZotRegistry: {
	apiVersion: "zot.guardianintelligence.org/v1alpha1"
	kind:       "ZotRegistry"
	metadata:   #Metadata
	spec: {
		// Ephemeral derived state generated per-alloc into /alloc; not durable.
		configPath:   #AbsolutePath
		htpasswdPath: #AbsolutePath

		// Durable registry blob/manifest store; a host bind-mount that
		// survives alloc GC.
		storageDir: #AbsolutePath

		host:          #Host
		port:          int & >=1 & <=65535
		realm:         #NonEmptyString
		logLevel:      #NonEmptyString
		publisherUser: #NonEmptyString
	}
}
