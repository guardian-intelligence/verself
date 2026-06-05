package v1alpha1

#NonEmptyString: string & !=""
#Duration:       =~"^([0-9]+(ns|us|ms|s|m|h))+$"
#Mode:           =~"^0[0-7]{3}$"
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
	substrate: {
		stateDir: #NonEmptyString
	}
	access: {
		ssh: #SSHAccess
	}
	seed: #Seed
}

#SSHAccess: {
	host:            #NonEmptyString
	port:            int & >=1 & <=65535
	user:            #NonEmptyString
	knownHostsFile:  #NonEmptyString
	identityFile?:   #NonEmptyString
	connectTimeout?: #Duration
	wireguardFallback?: {
		host:       #NonEmptyString
		port:       int & >=1 & <=65535
		interface?: #NonEmptyString
	}
}

#Seed: {
	targetRoot: #NonEmptyString
	paths: [#SeedPath, ...#SeedPath]
}

#SeedPath: {
	source: #NonEmptyString
	target: #NonEmptyString
	mode:   #Mode
}

#NomadJobFile: {
	path: #NonEmptyString
	requiredFor?: [...("recovery" | "deploy" | "observe")]
}
