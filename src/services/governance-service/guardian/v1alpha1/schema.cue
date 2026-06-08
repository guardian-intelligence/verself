package v1alpha1

#NonEmptyString: string & !=""
#HTTPSURL:       =~"^https://[^\\s]+$"
#AbsolutePath:   =~"^/[^\\s]*$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#CredentialRef: #ObjectRef & {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "SecretPath"
}

#GovernanceService: {
	apiVersion: "governance.guardianintelligence.org/v1alpha1"
	kind:       "GovernanceService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		environment:    #NonEmptyString
		publicBaseURL:  #HTTPSURL
		writer: {
			instanceID: #NonEmptyString
		}
		auth: {
			issuerURL:   #HTTPSURL
			audienceRef: #CredentialRef
		}
		apiActivity: {
			hmacKeyID:  #NonEmptyString
			hmacKeyRef: #CredentialRef
		}
		postgres: {
			dsn:      #NonEmptyString
			maxConns: int & >0
			identity: {
				dsn:      #NonEmptyString
				maxConns: int & >0
			}
			billing: {
				dsn:      #NonEmptyString
				maxConns: int & >0
			}
			sandbox: {
				dsn:      #NonEmptyString
				maxConns: int & >0
			}
		}
		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}
		exports: {
			dir:      #AbsolutePath
			ttlHours: int & >0
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
