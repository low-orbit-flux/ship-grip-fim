Integrity

New todo items ( 2021 )

    - comparison reports should be saved to a DB 
    - compare should also be parallel
	- print header when reading report or comparing reports
	- option to just print header
	- how is white space in a file name handled
	- clean up CLi / add options ( path, etc )
	- run as daemon with scheduled reports
		- add alerts ( email and send to ship grip alert system )
		- scheudle / view runs
	- GUI
		- Wrap cli
		- view jobs
		- schedule jobs
	- search single file accross reports
	- optional swap path variables for report comparison





   - do something about trailing slashes when paths are concatenated
   - Auto Compare
	     - figure out the newest and second newest reports
	     - automatically check for changes in last two runs
			 - do this for last two runs with same report name
			    (ex: so only reports with name "nightly scheduled" will be compared and not "adhoc report" )
	 - GUI
	 - daemon status viewer
	 - report viewer

	 - not searching for new files for efficiency,
	 				maybe think about this later as an option

	 - ctime, atime etc
	 

Build and Run:

 go build .
 ./ship-grip-fim scan .


Features:
	- only the first comma is split on
