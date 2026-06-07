package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"

#Metadata: {
	name: #NonEmptyString
}

#ProfileService: {
	apiVersion: "profile.guardianintelligence.org/v1alpha1"
	kind:       "ProfileService"
	metadata:   #Metadata
	spec: {
		auth: {
			issuerURL: #HTTPSURL
			audience:  #NonEmptyString
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
