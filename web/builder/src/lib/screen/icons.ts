// The icons builders can put on buttons, stats and list items.
import type { Component } from 'svelte'
import ActivityIcon from '@lucide/svelte/icons/activity'
import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left'
import ArrowRightIcon from '@lucide/svelte/icons/arrow-right'
import BellIcon from '@lucide/svelte/icons/bell'
import BookmarkIcon from '@lucide/svelte/icons/bookmark'
import BoxIcon from '@lucide/svelte/icons/box'
import BriefcaseIcon from '@lucide/svelte/icons/briefcase'
import CalendarIcon from '@lucide/svelte/icons/calendar'
import ChartColumnIcon from '@lucide/svelte/icons/chart-column'
import CheckIcon from '@lucide/svelte/icons/check'
import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
import ClockIcon from '@lucide/svelte/icons/clock'
import CreditCardIcon from '@lucide/svelte/icons/credit-card'
import DollarSignIcon from '@lucide/svelte/icons/dollar-sign'
import DownloadIcon from '@lucide/svelte/icons/download'
import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
import EyeIcon from '@lucide/svelte/icons/eye'
import FileTextIcon from '@lucide/svelte/icons/file-text'
import FolderIcon from '@lucide/svelte/icons/folder'
import GlobeIcon from '@lucide/svelte/icons/globe'
import HeartIcon from '@lucide/svelte/icons/heart'
import HouseIcon from '@lucide/svelte/icons/house'
import ImageIcon from '@lucide/svelte/icons/image'
import InfoIcon from '@lucide/svelte/icons/info'
import LayersIcon from '@lucide/svelte/icons/layers'
import LinkIcon from '@lucide/svelte/icons/link'
import ListIcon from '@lucide/svelte/icons/list'
import LockIcon from '@lucide/svelte/icons/lock'
import LogOutIcon from '@lucide/svelte/icons/log-out'
import MailIcon from '@lucide/svelte/icons/mail'
import MapPinIcon from '@lucide/svelte/icons/map-pin'
import MessageSquareIcon from '@lucide/svelte/icons/message-square'
import PackageIcon from '@lucide/svelte/icons/package'
import PencilIcon from '@lucide/svelte/icons/pencil'
import PhoneIcon from '@lucide/svelte/icons/phone'
import PrinterIcon from '@lucide/svelte/icons/printer'
import PaperclipIcon from '@lucide/svelte/icons/paperclip'
import PlusIcon from '@lucide/svelte/icons/plus'
import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
import SaveIcon from '@lucide/svelte/icons/save'
import SearchIcon from '@lucide/svelte/icons/search'
import SendIcon from '@lucide/svelte/icons/send'
import SettingsIcon from '@lucide/svelte/icons/settings'
import ShoppingCartIcon from '@lucide/svelte/icons/shopping-cart'
import SquarePenIcon from '@lucide/svelte/icons/square-pen'
import StarIcon from '@lucide/svelte/icons/star'
import TagIcon from '@lucide/svelte/icons/tag'
import TrendingDownIcon from '@lucide/svelte/icons/trending-down'
import TrendingUpIcon from '@lucide/svelte/icons/trending-up'
import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
import TruckIcon from '@lucide/svelte/icons/truck'
import UploadIcon from '@lucide/svelte/icons/upload'
import UserIcon from '@lucide/svelte/icons/user'
import UsersIcon from '@lucide/svelte/icons/users'
import WalletIcon from '@lucide/svelte/icons/wallet'
import XIcon from '@lucide/svelte/icons/x'
import ZapIcon from '@lucide/svelte/icons/zap'
import TrashIcon from '@lucide/svelte/icons/trash'
import FunnelIcon from '@lucide/svelte/icons/funnel'
import BuildingIcon from '@lucide/svelte/icons/building'

export const icons: Record<string, Component<any>> = {
  'activity': ActivityIcon,
  'arrow-left': ArrowLeftIcon,
  'arrow-right': ArrowRightIcon,
  'bell': BellIcon,
  'bookmark': BookmarkIcon,
  'box': BoxIcon,
  'briefcase': BriefcaseIcon,
  'calendar': CalendarIcon,
  'chart-column': ChartColumnIcon,
  'check': CheckIcon,
  'chevron-right': ChevronRightIcon,
  'circle-alert': CircleAlertIcon,
  'circle-check': CircleCheckIcon,
  'clock': ClockIcon,
  'credit-card': CreditCardIcon,
  'dollar-sign': DollarSignIcon,
  'download': DownloadIcon,
  'external-link': ExternalLinkIcon,
  'eye': EyeIcon,
  'file-text': FileTextIcon,
  'folder': FolderIcon,
  'globe': GlobeIcon,
  'heart': HeartIcon,
  'house': HouseIcon,
  'image': ImageIcon,
  'info': InfoIcon,
  'paperclip': PaperclipIcon,
  'printer': PrinterIcon,
  'layers': LayersIcon,
  'link': LinkIcon,
  'list': ListIcon,
  'lock': LockIcon,
  'log-out': LogOutIcon,
  'mail': MailIcon,
  'map-pin': MapPinIcon,
  'message-square': MessageSquareIcon,
  'package': PackageIcon,
  'pencil': PencilIcon,
  'phone': PhoneIcon,
  'plus': PlusIcon,
  'refresh-cw': RefreshCwIcon,
  'save': SaveIcon,
  'search': SearchIcon,
  'send': SendIcon,
  'settings': SettingsIcon,
  'shopping-cart': ShoppingCartIcon,
  'square-pen': SquarePenIcon,
  'star': StarIcon,
  'tag': TagIcon,
  'trending-down': TrendingDownIcon,
  'trending-up': TrendingUpIcon,
  'triangle-alert': TriangleAlertIcon,
  'truck': TruckIcon,
  'upload': UploadIcon,
  'user': UserIcon,
  'users': UsersIcon,
  'wallet': WalletIcon,
  'x': XIcon,
  'zap': ZapIcon,
  'trash': TrashIcon,
  'funnel': FunnelIcon,
  'building': BuildingIcon,
}

export const iconNames = Object.keys(icons).sort()
