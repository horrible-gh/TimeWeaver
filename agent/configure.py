from util import jsonutil
import LogAssist.log as Logger

server_config = jsonutil.json_read("conf/server.json")
Logger.logger_init(server_config.get("log", None))

time_weaver_db = server_config['databases']['time_weaver']
time_weaver_config = jsonutil.json_read("conf/time_weaver.json")
version = jsonutil.json_read("conf/version.json")
