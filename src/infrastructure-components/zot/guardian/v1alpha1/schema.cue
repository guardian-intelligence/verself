package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString

#Metadata: {
	name: #NonEmptyString
}

#ZotRegistry: {
	apiVersion: "zot.guardianintelligence.org/v1alpha1"
	kind:       "ZotRegistry"
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

		host:          #Host
		port:          int & >=1 & <=65535
		realm:         #NonEmptyString
		logLevel:      #NonEmptyString
		publisherUser: #NonEmptyString
	}
}
