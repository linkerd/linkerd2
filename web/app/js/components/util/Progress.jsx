import LinearProgress from '@material-ui/core/LinearProgress';
import { dashboardTheme } from './theme.js';
import { withStyles } from '@material-ui/core/styles';

const colorLookup = {
  good: {
    colorPrimary: dashboardTheme.status.light.good, // background bar color (lighter)
    barColorPrimary: dashboardTheme.status.dark.good, // inner bar color (darker)
  },
  warning: {
    colorPrimary: dashboardTheme.status.light.warning,
    barColorPrimary: dashboardTheme.status.dark.warning,
  },
  poor: {
    colorPrimary: dashboardTheme.status.light.danger,
    barColorPrimary: dashboardTheme.status.dark.danger,
  },
  default: {
    colorPrimary: dashboardTheme.status.light.default,
    barColorPrimary: dashboardTheme.status.dark.default,
  }
};

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
