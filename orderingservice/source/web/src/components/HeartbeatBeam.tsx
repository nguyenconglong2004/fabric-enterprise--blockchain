import { motion } from 'framer-motion'
import { type HbBeam } from '../store/cluster'

interface Props {
  beam: HbBeam
  fromX: number
  fromY: number
  toX: number
  toY: number
}

export function HeartbeatBeam({ beam, fromX, fromY, toX, toY }: Props) {
  return (
    <motion.circle
      key={beam.id}
      r={5}
      fill="#F59E0B"
      opacity={0.9}
      initial={{ cx: fromX, cy: fromY, opacity: 1 }}
      animate={{ cx: toX, cy: toY, opacity: 0 }}
      transition={{ duration: 0.5, ease: 'easeOut' }}
    />
  )
}
