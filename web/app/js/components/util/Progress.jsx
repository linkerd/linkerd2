import LinearProgress from '@material-ui/core/LinearProgress';
import { withStyles } from '@material-ui/core/styles';

const colorLookup = {
  good: {
    colorPrimary: '#c8e6c9', // background bar color (lighter)
    barColorPrimary: '#388e3c', // inner bar color (darker)
  },
  neutral: {
    colorPrimary: '#ffcc80',
    barColorPrimary: '#ef6c00',
  },
  poor: {
    colorPrimary: '#ffebee',
    barColorPrimary: '#d32f2f',
  },
  default: {
    colorPrimary: '#e8eaf6',
    barColorPrimary: '#3f51b5',
  }
}

export const StyledProgress = (classification = "default") => withStyles({
  root: {
    flexGrow: 1,
  },
  colorPrimary: {
    backgroundColor: colorLookup[classification].colorPrimary,
  },
  barColorPrimary: {
    backgroundColor: colorLookup[classification].barColorPrimary,
  },
})(LinearProgress);
