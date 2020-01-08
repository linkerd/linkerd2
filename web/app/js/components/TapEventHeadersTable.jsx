import ListItem from '@material-ui/core/ListItem';
import ListItemText from '@material-ui/core/ListItemText';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';

const headersStyles = {
  headerName: {
    fontSize: '12px',
    marginTop: '5px',
  },
};

const HeadersContentBase = ({ headers, classes }) => {
  return (
    <React.Fragment>
      {headers.map(header => {
        return (
          <React.Fragment key={`${header.name}_${header.valueStr}`}>
            <Typography
              className={classes.headerName}
              variant="inherit"
              display="block"
              color="textPrimary">
              {header.name}
            </Typography>
            <Typography
              variant="inherit"
              color="textSecondary">
              {header.valueStr}
            </Typography>
          </React.Fragment>
        );
      })}
    </React.Fragment>
  );
};

HeadersContentBase.propTypes = {
  headers: PropTypes.arrayOf(PropTypes.shape({
    name: PropTypes.string.isRequired,
    valueStr: PropTypes.string.isRequired,
  })).isRequired,
};

const HeadersContentDisplay = withStyles(headersStyles)(HeadersContentBase);

export const headersDisplay = (title, value) => {
  if (!value) {
    return null;
  }

  return (
    <ListItem disableGutters>
      <ListItemText
        primary={title}
        secondary={'headers' in value ? <HeadersContentDisplay headers={value.headers} /> : '-'} />
    </ListItem>
  );
};
