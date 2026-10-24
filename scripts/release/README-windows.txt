Pimpbase {{VERSION}} for Windows
================================

Pimpbase is one program: no database server, Node or Docker to install.


Start it
--------

Double-click start-pimpbase.bat. It starts the server and opens
http://localhost:8080 in your browser. The first visit asks you to create
the owner account. Keep the black window open while you use Pimpbase;
close it, or press Ctrl+C in it, to stop the server.

Windows may warn that the program is from an unknown publisher, because it
is not signed: choose "More info", then "Run anyway". Windows Firewall may
ask whether to allow it on networks; allow "Private networks" if other
computers should reach it, otherwise cancel and it still works on this PC.

From PowerShell or Command Prompt you can pass flags yourself:

    .\pimpbase.exe serve --addr :9090 --data C:\Pimpbase\data
    .\pimpbase.exe serve -h          (every flag)
    .\pimpbase.exe version


Where your data lives
---------------------

start-pimpbase.bat keeps everything in the "data" folder next to it:

    data\pimpbase.db        users, apps, screens, flows, settings
    data\apps\<id>.db       the rows and uploaded files of each app

pimpbase.exe run by hand uses a "data" folder in the current folder, or the
folder given with --data or the PIMPBASE_DATA environment variable.

Move or copy the data folder only while Pimpbase is stopped, or make a
backup while it runs:

    .\pimpbase.exe backup --data data --out backup.zip
    .\pimpbase.exe restore --data C:\new\data backup.zip

Avoid keeping the data folder in OneDrive, Dropbox or another synced
folder: syncing a database while it is written can damage it.


Start with Windows
------------------

Pimpbase does not install itself as a Windows service yet. To start it when
the computer starts, open Task Scheduler, create a task that runs
"At startup" whether a user is signed in or not, with the action

    Program:   C:\Pimpbase\pimpbase.exe
    Arguments: serve --data C:\Pimpbase\data
    Start in:  C:\Pimpbase

(change C:\Pimpbase to where you unzipped it), and untick "Stop the task if
it runs longer than".


More
----

Every flag also reads an environment variable (PIMPBASE_ADDR,
PIMPBASE_DATA, PIMPBASE_POSTGRES, ...). The full guide is the README in the
source repository.
