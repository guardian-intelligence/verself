package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#MemorySize:     =~"^[0-9]+(b|kb|mb|gb|tb)$"

#Metadata: {
	name: #NonEmptyString
}

#VerdaccioRegistry: {
	apiVersion: "verdaccio.guardianintelligence.org/v1alpha1"
	kind:       "VerdaccioRegistry"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		storageDir:      #AbsolutePath
		htpasswdPath:    #AbsolutePath
		reportPath:      #AbsolutePath

		user:  #NonEmptyString
		group: #NonEmptyString

		host:        #Host
		port:        int & >=1 & <=65535
		maxBodySize: #MemorySize
		uplink: {
			name:      #NonEmptyString
			url:       =~"^https://[^\\s]+$"
			cache:     bool
			maxAge:    #NonEmptyString
			strictSSL: bool
		}
		packageFilter: {
			minAgeDays: int & >=0
		}
		log: {
			level: #NonEmptyString
		}
	}
}
