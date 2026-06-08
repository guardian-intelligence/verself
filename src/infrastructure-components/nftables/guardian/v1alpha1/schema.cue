package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""

#Metadata: {
	name: #NonEmptyString
}

#NftablesFirewall: {
	apiVersion: "nftables.guardianintelligence.org/v1alpha1"
	kind:       "NftablesFirewall"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		rulesDir:        #AbsolutePath
		manageSystemd:   bool
		systemd: {
			serviceUnitPath:    #AbsolutePath
			firewallTargetPath: #AbsolutePath
		}
	}
}
