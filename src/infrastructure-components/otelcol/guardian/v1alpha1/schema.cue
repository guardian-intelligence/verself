package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""

#Metadata: {
	name: #NonEmptyString
}

#OtelCollector: {
	apiVersion: "otelcol.guardianintelligence.org/v1alpha1"
	kind:       "OtelCollector"
	metadata:   #Metadata
	spec: {
		runtimeArtifact:     #RepoPath
		configArtifact:      #RepoPath
		runtimeRoot:         #AbsolutePath
		configRoot:          #AbsolutePath
		configPath:          #AbsolutePath
		helperConfigPath:    #AbsolutePath
		dataDir:             #AbsolutePath
		storageDir:          #AbsolutePath
		clickhouseSpiffeDir: #AbsolutePath
		reportPath:          #AbsolutePath

		user:  #NonEmptyString
		group: #NonEmptyString
		supplementaryGroups: [...#NonEmptyString]
	}
}
