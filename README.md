
    Service ( agent )
	    - need port and IP (interface ) set from config params
	    - CLI:   "ship-grip-fim remote host port command"

		- list reports returns imediately
		- scan runs in BG ( need to break this off )
		    - if still connected receive message back
        - show scan is currently running  ( scan status command )
		- don't run when a scan or compare is already running
		

	    - CLI can connect to service ( all normal functionality )
		- option to save reports locally ( sync reports )
		- CLI can have a server list
		- CLI can start service on host
		- Check status/connectivity of all services
		- bulk request


	- change logo

	
	GUI
	    - duplicate CLI functionality ( include connect to server )

    Schedule	
	    - schedule defined on agent side so it will run even if control server doesn't watch it
		- view running jobs ( 1 per host at a time )
		- schedule jobs ( don't run if running )
		- view scheduled

	Alerts
		- add alerts ( email and send to ship grip alert system )

    Auto Compare
	     - figure out the newest and second newest reports
	     - automatically check for changes in last two runs
			 - do this for last two runs with same report name
			    (ex: so only reports with name "nightly scheduled" will be compared and not "adhoc report" )

    - One report / table combining multiple reports from differenet hosts ( newest from each )


   Future features:
   
    - encrypted connection
	- pass configuration to agents remotely
	- test on Windows
   	- propper logging
    - compare should also be parallel
	- search single file accross reports
	- monitor permissions / owner ( Linux, Windows, etc. )
	- security queries for permissions ( search for writable and SUID, etc )
	- specify multiple paths to check for report ( this will make base path var messy, just run separate instances for now )

Architecture:

    docs/architecture-2022.jpeg is a photo of the original 2022 module diagram
    (data_file / data_mongo / data / integrity / logic).  The agent, scheduler,
    exporter and GUI files were added later and are not on it.

Build and Run:

 scripts/build.sh              # vet + test + build into build/ship-grip-fim
 ./build/ship-grip-fim scan .

 scripts/deploy.sh [alias]     # push the binary to hosts.conf hosts over SSH,
                               # restart their agents and pin their TLS fingerprints
 scripts/ship-grip-fim-agent.service   # systemd unit for the agent

Security ( see howto.md, "Security" ):

 - agents and the web GUI speak TLS 1.3; clients pin each agent's certificate
   fingerprint in known_agents ( like ssh known_hosts )
 - agents and the web GUI require a login with roles ro / rw / admin;
   a fresh install creates admin / changeme - change it:
     ./ship-grip-fim user passwd admin <new-password>            ( local file )
     ./ship-grip-fim remote <host> <port> user passwd admin <new-password>
 - run one central web GUI ( webHost="0.0.0.0" ) for people who don't want the
   desktop GUI; the desktop GUI works from anywhere with agent credentials


Features:
	- only the first comma is split on
