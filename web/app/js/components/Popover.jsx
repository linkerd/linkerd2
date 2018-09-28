/* eslint-disable jsx-a11y/mouse-events-have-key-events */
import Popover from '@material-ui/core/Popover';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  popover: {
    pointerEvents: 'none',
  },
  paper: {
    padding: theme.spacing.unit,
  },
});

class MouseOverPopover extends React.Component {
  state = {
    anchorEl: null,
  };

  handlePopoverOpen = event => {
    this.setState({ anchorEl: event.currentTarget });
  };

  handlePopoverClose = () => {
    this.setState({ anchorEl: null });
  };

  render() {
    const { classes, baseContent, popoverContent } = this.props;
    const { anchorEl } = this.state;
    const open = Boolean(anchorEl);

    return (
      <div>
        <div
          aria-owns={open ? 'mouse-over-popover' : null}
          aria-haspopup="true"
          onMouseEnter={this.handlePopoverOpen}
          onMouseLeave={this.handlePopoverClose}>
          {baseContent}
        </div>
        <Popover
          id="mouse-over-popover"
          className={classes.popover}
          classes={{
            paper: classes.paper,
          }}
          open={open}
          anchorEl={anchorEl}
          anchorOrigin={{
            vertical: 'bottom',
            horizontal: 'left',
          }}
          transformOrigin={{
            vertical: 'top',
            horizontal: 'left',
          }}
          onClose={this.handlePopoverClose}
          disableRestoreFocus>
          {popoverContent}
        </Popover>
      </div>
    );
  }
}

MouseOverPopover.propTypes = {
  baseContent: PropTypes.node.isRequired,
  classes: PropTypes.shape({}).isRequired,
  popoverContent: PropTypes.node.isRequired
};

export default withStyles(styles)(MouseOverPopover);
