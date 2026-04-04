/*
 * Vala bindings for libmosquitto (Eclipse Mosquitto MQTT client library).
 * Covers the subset needed by the MU Rhythmbox plugin.
 */

[CCode (cheader_filename = "mosquitto.h")]
namespace Mosquitto {

    [CCode (cname = "mosq_err_t", cprefix = "MOSQ_ERR_", has_type_id = false)]
    public enum Error {
        SUCCESS = 0,
        NOMEM = 1,
        PROTOCOL = 2,
        INVAL = 3,
        NO_CONN = 4,
        CONN_REFUSED = 5,
        NOT_FOUND = 6,
        CONN_LOST = 7,
        TLS = 8,
        PAYLOAD_SIZE = 9,
        NOT_SUPPORTED = 10,
        AUTH = 11,
        ACL_DENIED = 12,
        UNKNOWN = 13,
        ERRNO = 14,
        EAI = 15,
        PROXY = 16,
        KEEPALIVE = 19,
        TIMEOUT = 27
    }

    [CCode (cname = "struct mosquitto_message", destroy_function = "", has_type_id = false)]
    public struct Message {
        public int mid;
        public unowned string topic;
        [CCode (array_length_cname = "payloadlen", array_length_type = "int")]
        public unowned uint8[] payload;
        public int payloadlen;
        public int qos;
        public bool retain;
    }

    [CCode (cname = "mosquitto_lib_init")]
    public static int lib_init ();

    [CCode (cname = "mosquitto_lib_cleanup")]
    public static int lib_cleanup ();

    [CCode (cname = "mosquitto_strerror")]
    public static unowned string strerror (int mosq_errno);

    [CCode (cname = "struct mosquitto", free_function = "mosquitto_destroy", has_type_id = false)]
    [Compact]
    public class Client {
        [CCode (cname = "mosquitto_new")]
        public Client (string? id, bool clean_session, void* userdata = null);

        [CCode (cname = "mosquitto_will_set")]
        public int will_set (string topic, int payloadlen, [CCode (array_length = false)] uint8[]? payload, int qos, bool retain);

        [CCode (cname = "mosquitto_username_pw_set")]
        public int username_pw_set (string? username, string? password);

        [CCode (cname = "mosquitto_connect")]
        public int connect (string host, int port, int keepalive);

        [CCode (cname = "mosquitto_connect_async")]
        public int connect_async (string host, int port, int keepalive);

        [CCode (cname = "mosquitto_reconnect_delay_set")]
        public int reconnect_delay_set (uint reconnect_delay, uint reconnect_delay_max, bool reconnect_exponential_backoff);

        [CCode (cname = "mosquitto_disconnect")]
        public int disconnect ();

        [CCode (cname = "mosquitto_subscribe")]
        public int subscribe (out int mid, string sub, int qos);

        [CCode (cname = "mosquitto_unsubscribe")]
        public int unsubscribe (out int mid, string sub);

        [CCode (cname = "mosquitto_publish")]
        public int publish (out int mid, string topic, int payloadlen, [CCode (array_length = false)] uint8[]? payload, int qos, bool retain);

        [CCode (cname = "mosquitto_loop_start")]
        public int loop_start ();

        [CCode (cname = "mosquitto_loop_stop")]
        public int loop_stop (bool force);

        [CCode (cname = "mosquitto_connect_callback_set")]
        public void connect_callback_set ([CCode (delegate_target = false)] ConnectCallback cb);

        [CCode (cname = "mosquitto_disconnect_callback_set")]
        public void disconnect_callback_set ([CCode (delegate_target = false)] DisconnectCallback cb);

        [CCode (cname = "mosquitto_message_callback_set")]
        public void message_callback_set ([CCode (delegate_target = false)] MessageCallback cb);
    }

    [CCode (cname = "on_connect_cb", has_target = false)]
    public delegate void ConnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "on_disconnect_cb", has_target = false)]
    public delegate void DisconnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "on_message_cb", has_target = false)]
    public delegate void MessageCallback (Client mosq, void* userdata, Message msg);
}
