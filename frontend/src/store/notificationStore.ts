import { create } from 'zustand'

export interface Notification {
    id: string
    type: 'info' | 'success' | 'warning' | 'error'
    title: string
    message: string
    time: string
    read: boolean
}

interface NotificationState {
    notifications: Notification[]
    addNotification: (notification: Omit<Notification, 'id' | 'time' | 'read'>) => void
    markAsRead: (id: string) => void
    markAllAsRead: () => void
    clearNotifications: () => void
}

export const useNotificationStore = create<NotificationState>((set) => ({
    notifications: [],

    addNotification: (data) => set((state) => {
        const newNotification: Notification = {
            ...data,
            id: Math.random().toString(36).substring(2, 9),
            time: '刚刚',
            read: false,
        }
        return { notifications: [newNotification, ...state.notifications] }
    }),

    markAsRead: (id) => set((state) => ({
        notifications: state.notifications.map((n) =>
            n.id === id ? { ...n, read: true } : n
        ),
    })),

    markAllAsRead: () => set((state) => ({
        notifications: state.notifications.map((n) => ({ ...n, read: true })),
    })),

    clearNotifications: () => set({ notifications: [] }),
}))
