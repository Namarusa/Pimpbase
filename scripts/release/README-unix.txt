Pimpbase {{VERSION}}
====================

Pimpbase is one program: no database server, Node or Docker to install.

    ./pimpbase serve        start it on http://localhost:8080
    ./pimpbase serve -h     every flag
    ./pimpbase version

The first visit asks you to create the owner account.

Data lives in ./data, relative to the folder you start it from, or in the
folder given with --data or PIMPBASE_DATA:

    data/pimpbase.db        users, apps, screens, flows, settings
    data/apps/<id>.db       the rows and uploaded files of each app

Back up while it runs with "./pimpbase backup --out backup.zip", and put a
backup back with "./pimpbase restore --data <empty folder> backup.zip".

On macOS, the first start may be blocked because the program is not signed:
run "xattr -d com.apple.quarantine pimpbase" once, or allow it in
System Settings, Privacy & Security.
