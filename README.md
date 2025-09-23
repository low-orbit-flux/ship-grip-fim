
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

Build and Run:

 go build .
 ./ship-grip-fim scan .


Features:
	- only the first comma is split on
