Integrity


	- remove base path before compare
	- check if files are in new files list, note as MOVED in report
	- ignore list ( ex. exclude .DS_Store )
    - specify multiple paths to check for report
	- specify config file as CLI arg

    Service ( agent )
	    - CLI can connect to service ( all normal functionality )
		- option to save reports locally ( sync reports )
		- CLI can have a server list
		- CLI can start service on host
		- Check status/connectivity of all services
		- bulk request

	
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

    - compare should also be parallel
	- search single file accross reports
	- ctime, atime etc
	 

Build and Run:

 go build .
 ./ship-grip-fim scan .


Features:
	- only the first comma is split on
