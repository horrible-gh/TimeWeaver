import time
from configure import time_weaver_db as twdb
from sqloader.init import database_init
import services.time_weaver.app as service

twdb_config = twdb['database']
db_instance, sqloader, migrator = database_init(twdb_config)

service.set_instances(db_instance, sqloader)
service.task_initializer()
service.task_rescheduler()

while True:
    time.sleep(60)