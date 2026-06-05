package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s/$.?#].[^\\s]*$"

#Document: #FlyProcedure

#FlyProcedure: {
	kind:  "FlyProcedure"
	name?: #NonEmptyString
	staticConfig: {
		baseURL:        #HTTPSURL
		credentialsRef: #NonEmptyString
	}
	board: #Board
	nomad: {
		address:   #NonEmptyString
		namespace: #NonEmptyString
		jobs: [#NomadJobFile, ...#NomadJobFile]
	}
}

#Board: {
	access: #LifecycleHook
	upload: #Upload
}

#Upload: {
	run:    #LifecycleHook
	verify: #LifecycleHook
}

#LifecycleHook: {
	argv: [#NonEmptyString, ...#NonEmptyString]
}

#NomadJobFile: {
	path: #NonEmptyString
	requiredFor?: [...("recovery" | "deploy" | "observe")]
}
