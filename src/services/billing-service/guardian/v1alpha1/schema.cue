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

#BillingService: {
	apiVersion: "billing.guardianintelligence.org/v1alpha1"
	kind:       "BillingService"
	metadata:   #Metadata
	spec: {
		installationID: #NonEmptyString
		auth: {
			issuerURL:   #HTTPSURL
			audienceRef: #CredentialRef
		}
		postgres: {
			dsn:                    #NonEmptyString
			maxConns:               int & >0
			minConns:               int & >=0
			maxConnLifetimeSeconds: int & >0
			maxConnIdleSeconds:     int & >0
		}
		clickhouse: {
			address:    #NonEmptyString
			user:       #NonEmptyString
			caCertPath: #AbsolutePath
		}
		tigerbeetle: {
			addresses: [#NonEmptyString, ...#NonEmptyString]
			clusterID: int & >=0
		}
		billing: {
			returnOrigins: [#HTTPSURL, ...#HTTPSURL]
		}
		stripe: {
			secretKeyRef:     #CredentialRef
			webhookSecretRef: #CredentialRef
		}
		spiffe: {
			endpointSocket: #NonEmptyString
		}
	}
}
