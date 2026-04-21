import green from '@material-ui/core/colors/green';
import grey from '@material-ui/core/colors/grey';
import orange from '@material-ui/core/colors/orange';
import red from '@material-ui/core/colors/red';

const status = {
  // custom variables for success rate indicators
  dark: {
    danger: red[500],
    warning: orange[500],
    good: green[500],
    default: grey[500],
  },
  // custom variables for progress bars, which need both the normal colors
  // as well as a lighter version of them for the bar background
  light: {
    danger: red[200],
    warning: orange[200],
    good: green[200],
    default: grey[200],
  },
};

export const dashboardTheme = {
  palette: {
    primary: {
      main: '#001443',
    },
  },
  // substituting default Material breakpoints with Bootstrap breakpoints
  breakpoints: {
    values: {
      sm: 576,
      md: 992,
      lg: 1200,
    },
  },
  status,
};

export const statusClassNames = theme => {
  theme.status = theme.status || status; // tests don't inject custom variables

  return {
    poor: {
      backgroundColor: theme.status.dark.danger,
    },
    warning: {
      backgroundColor: theme.status.dark.warning,
    },
    good: {
      backgroundColor: theme.status.dark.good,
    },
    default: {
      backgroundColor: theme.status.dark.default,
    },
  };
};
