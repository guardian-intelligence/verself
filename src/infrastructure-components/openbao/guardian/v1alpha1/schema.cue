package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#HTTPURL:        =~"^https?://[^\\s]+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#OpenBaoCluster: {
	apiVersion: "openbao.guardianintelligence.org/v1alpha1"
	kind:       "OpenBaoCluster"
	metadata:   #Metadata
	spec: {
		address:     #HTTPURL
		caCert:      #AbsolutePath
		runtimeRoot: #AbsolutePath
		dataDir:     #AbsolutePath
		configPath:  #AbsolutePath
		reportPath:  #AbsolutePath
		initMaterialPath: #AbsolutePath
		loopInterval: #NonEmptyString
		seal: {
			shamir: {
				keyShares:    int & >0
				keyThreshold: int & >0 & <=keyShares
				pgpRecipientRefs: [...#ObjectRef]
				rootTokenRecipientRef?: #ObjectRef
			}
		}
		snapshots?: {
			restore?: {
				manifestPath?: #AbsolutePath
				snapshotPath?: #AbsolutePath
				sourceRef?:    #ObjectRef
			}
			save?: {
				manifestPath:    #AbsolutePath
				snapshotPath:    #AbsolutePath
				destinationRef?: #ObjectRef
			}
		}
		baseline?: {
			reconcile: bool
			operatorTokenRequired?: bool
		}
	}
}
