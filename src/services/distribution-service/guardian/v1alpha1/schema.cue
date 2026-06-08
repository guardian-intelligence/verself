package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#AbsolutePath:   =~"^/[^\\s]*$"

#Metadata: {
	name: #NonEmptyString
}

#DistributionService: {
	apiVersion: "distribution.guardianintelligence.org/v1alpha1"
	kind:       "DistributionService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		auth: {
			issuerURL: #HTTPSURL
			audience:  #NonEmptyString
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
		}
		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}
		attestation: {
			trustedBuilders: [#NonEmptyString, ...#NonEmptyString]
			trustedSigners: [#NonEmptyString, ...#NonEmptyString]
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
