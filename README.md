Integrity


    - ignore list ( ex. exclude .DS_Store )
	- print header when reading report or comparing reports


	- run as daemon with scheduled reports
		- add alerts ( email and send to ship grip alert system )
		- scheudle / view runs
	GUI
		- Wrap cli

	Schedule	
		- view running jobs ( 1 per host at a time )
		- schedule jobs ( don't run if running )
		- view scheduled


   - Auto Compare
	     - figure out the newest and second newest reports
	     - automatically check for changes in last two runs
			 - do this for last two runs with same report name
			    (ex: so only reports with name "nightly scheduled" will be compared and not "adhoc report" )

	 - daemon status viewer
	 - report viewer


	- TEST - optional swap path variables for report comparison
		    - ???? do something about trailing slashes when paths are concatenated
    - compare should also be parallel
	- search single file accross reports
	- ctime, atime etc
	 

Build and Run:

 go build .
 ./ship-grip-fim scan .


Features:
	- only the first comma is split on
