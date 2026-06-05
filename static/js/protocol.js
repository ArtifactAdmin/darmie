/** Protocol message type constants (mirrors internal/protocol/protocol.go). */
export const MSG = Object.freeze({
    // Client 
    REGISTER:      'register',
    LOGIN:         'login',
    LIST_ROOMS:    'list_rooms',
    CREATE_ROOM:   'create_room',
    JOIN_ROOM:     'join_room',
    LEAVE_ROOM:    'leave_room',
    TEXT_MESSAGE:  'text_message',
    OFFER:         'offer',
    ANSWER:        'answer',
    ICE_CANDIDATE: 'ice_candidate',
    VIDEO_STOPPED: 'video_stopped',

    // Server 
    AUTH_SUCCESS:  'auth_success',
    AUTH_ERROR:    'auth_error',
    ROOM_LIST:     'room_list',
    ROOM_JOINED:   'room_joined',
    ROOM_LEFT:     'room_left',
    ROOM_CREATED:  'room_created',
    USER_JOINED:   'user_joined',
    USER_LEFT:     'user_left',
    ERROR:         'error',

    // Internal pseudo-event
    DISCONNECTED:  '__close__',
});
