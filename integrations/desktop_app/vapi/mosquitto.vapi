/* mosquitto.vapi — Vala bindings for libmosquitto 2.x */

[CCode (cheader_filename = "mosquitto.h")]
namespace Mosquitto {

    [CCode (cname = "mosquitto_lib_init")]
    public static int lib_init ();

    [CCode (cname = "mosquitto_lib_cleanup")]
    public static int lib_cleanup ();

    [CCode (cname = "mosquitto_lib_version")]
    public static int lib_version (out int major, out int minor, out int revision);

    [CCode (cname = "mosquitto_strerror")]
    public static unowned string strerror (int mosq_errno);

    [CCode (cname = "mosquitto_connack_string")]
    public static unowned string connack_string (int connack_code);

    /* Error codes */
    [CCode (cname = "MOSQ_ERR_SUCCESS")]
    public const int ERR_SUCCESS;
    [CCode (cname = "MOSQ_ERR_NOMEM")]
    public const int ERR_NOMEM;
    [CCode (cname = "MOSQ_ERR_PROTOCOL")]
    public const int ERR_PROTOCOL;
    [CCode (cname = "MOSQ_ERR_INVAL")]
    public const int ERR_INVAL;
    [CCode (cname = "MOSQ_ERR_NO_CONN")]
    public const int ERR_NO_CONN;
    [CCode (cname = "MOSQ_ERR_CONN_REFUSED")]
    public const int ERR_CONN_REFUSED;
    [CCode (cname = "MOSQ_ERR_NOT_FOUND")]
    public const int ERR_NOT_FOUND;
    [CCode (cname = "MOSQ_ERR_CONN_LOST")]
    public const int ERR_CONN_LOST;
    [CCode (cname = "MOSQ_ERR_TLS")]
    public const int ERR_TLS;
    [CCode (cname = "MOSQ_ERR_PAYLOAD_SIZE")]
    public const int ERR_PAYLOAD_SIZE;
    [CCode (cname = "MOSQ_ERR_NOT_SUPPORTED")]
    public const int ERR_NOT_SUPPORTED;
    [CCode (cname = "MOSQ_ERR_AUTH")]
    public const int ERR_AUTH;
    [CCode (cname = "MOSQ_ERR_ACL_DENIED")]
    public const int ERR_ACL_DENIED;
    [CCode (cname = "MOSQ_ERR_UNKNOWN")]
    public const int ERR_UNKNOWN;
    [CCode (cname = "MOSQ_ERR_ERRNO")]
    public const int ERR_ERRNO;
    [CCode (cname = "MOSQ_ERR_EAI")]
    public const int ERR_EAI;
    [CCode (cname = "MOSQ_ERR_PROXY")]
    public const int ERR_PROXY;
    [CCode (cname = "MOSQ_ERR_KEEPALIVE")]
    public const int ERR_KEEPALIVE;

    /* QoS levels */
    [CCode (cname = "int", cprefix = "", has_type_id = false)]
    public enum Qos {
        [CCode (cname = "0")]
        AT_MOST_ONCE,
        [CCode (cname = "1")]
        AT_LEAST_ONCE,
        [CCode (cname = "2")]
        EXACTLY_ONCE
    }

    /* Message struct */
    [CCode (cname = "struct mosquitto_message", destroy_function = "mosquitto_message_free", has_type_id = false)]
    public struct Message {
        public int mid;
        [CCode (cname = "topic")]
        public unowned string topic;
        [CCode (cname = "payload", array_length_cname = "payloadlen", array_length_type = "int")]
        public unowned uint8[] payload;
        public int qos;
        public bool retain;
    }

    /* Callback types */
    [CCode (cname = "on_connect_callback", has_target = false)]
    public delegate void ConnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "on_disconnect_callback", has_target = false)]
    public delegate void DisconnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "on_publish_callback", has_target = false)]
    public delegate void PublishCallback (Client mosq, void* userdata, int mid);

    [CCode (cname = "on_message_callback", has_target = false)]
    public delegate void MessageCallback (Client mosq, void* userdata, Message msg);

    [CCode (cname = "on_subscribe_callback", has_target = false)]
    public delegate void SubscribeCallback (Client mosq, void* userdata, int mid, int qos_count, [CCode (array_length = false)] int[] granted_qos);

    [CCode (cname = "on_unsubscribe_callback", has_target = false)]
    public delegate void UnsubscribeCallback (Client mosq, void* userdata, int mid);

    [CCode (cname = "on_log_callback", has_target = false)]
    public delegate void LogCallback (Client mosq, void* userdata, int level, string str);

    /* Client */
    [CCode (cname = "struct mosquitto", free_function = "mosquitto_destroy", has_type_id = false)]
    [Compact]
    public class Client {
        [CCode (cname = "mosquitto_new")]
        public Client (string? id = null, bool clean_session = true, void* userdata = null);

        [CCode (cname = "mosquitto_connect")]
        public int connect (string host, int port = 1883, int keepalive = 60);

        [CCode (cname = "mosquitto_connect_async")]
        public int connect_async (string host, int port = 1883, int keepalive = 60);

        [CCode (cname = "mosquitto_reconnect")]
        public int reconnect ();

        [CCode (cname = "mosquitto_reconnect_async")]
        public int reconnect_async ();

        [CCode (cname = "mosquitto_disconnect")]
        public int disconnect ();

        [CCode (cname = "mosquitto_publish")]
        public int publish (out int mid, string topic, [CCode (array_length_pos = 2.5, array_length_type = "int")] uint8[] payload, int qos = 0, bool retain = false);

        [CCode (cname = "mosquitto_subscribe")]
        public int subscribe (out int mid, string sub, int qos = 0);

        [CCode (cname = "mosquitto_unsubscribe")]
        public int unsubscribe (out int mid, string sub);

        [CCode (cname = "mosquitto_loop")]
        public int loop (int timeout = -1, int max_packets = 1);

        [CCode (cname = "mosquitto_loop_start")]
        public int loop_start ();

        [CCode (cname = "mosquitto_loop_stop")]
        public int loop_stop (bool force = false);

        [CCode (cname = "mosquitto_loop_forever")]
        public int loop_forever (int timeout = -1, int max_packets = 1);

        [CCode (cname = "mosquitto_will_set")]
        public int will_set (string topic, [CCode (array_length_pos = 1.5, array_length_type = "int")] uint8[] payload, int qos = 0, bool retain = false);

        [CCode (cname = "mosquitto_will_clear")]
        public int will_clear ();

        [CCode (cname = "mosquitto_username_pw_set")]
        public int username_pw_set (string? username, string? password);

        [CCode (cname = "mosquitto_tls_set")]
        public int tls_set (string? cafile, string? capath, string? certfile, string? keyfile, void* pw_callback = null);

        [CCode (cname = "mosquitto_tls_insecure_set")]
        public int tls_insecure_set (bool value);

        [CCode (cname = "mosquitto_connect_callback_set")]
        public void connect_callback_set (ConnectCallback? on_connect);

        [CCode (cname = "mosquitto_disconnect_callback_set")]
        public void disconnect_callback_set (DisconnectCallback? on_disconnect);

        [CCode (cname = "mosquitto_publish_callback_set")]
        public void publish_callback_set (PublishCallback? on_publish);

        [CCode (cname = "mosquitto_message_callback_set")]
        public void message_callback_set (MessageCallback? on_message);

        [CCode (cname = "mosquitto_subscribe_callback_set")]
        public void subscribe_callback_set (SubscribeCallback? on_subscribe);

        [CCode (cname = "mosquitto_unsubscribe_callback_set")]
        public void unsubscribe_set (UnsubscribeCallback? on_unsubscribe);

        [CCode (cname = "mosquitto_log_callback_set")]
        public void log_callback_set (LogCallback? on_log);

        [CCode (cname = "mosquitto_int_option")]
        public int int_option (int option, int value);

        [CCode (cname = "mosquitto_reconnect_delay_set")]
        public int reconnect_delay_set (uint delay, uint delay_max, bool exponential_backoff);

        [CCode (cname = "mosquitto_user_data_set")]
        public void user_data_set (void* userdata);

        [CCode (cname = "mosquitto_want_write")]
        public bool want_write ();

        [CCode (cname = "mosquitto_socket")]
        public int socket ();
    }
}
