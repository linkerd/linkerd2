import Popover from '@material-ui/core/Popover';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  paper: {
    padding: theme.spacing(1),
  },
});

class ClickablePopover extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      anchorEl: null,
    };
  }

  handleClick = event => {
    this.setState({ anchorEl: event.currentTarget });
  };

  handleKeyPress = event => {
    if (event.key === 'Enter') {
      this.setState({ anchorEl: event.currentTarget });
    }
  }

  handleClose = () => {
    this.setState({ anchorEl: null });
  };

  render() {
    const { classes, baseContent, popoverContent } = this.props;
    const { anchorEl } = this.state;
    const open = Boolean(anchorEl);

    return (
      <React.Fragment>
        <span
          aria-owns={open ? 'clickable-popover' : null}
          aria-haspopup="true"
          onClick={this.handleClick}
          onKeyPress={this.handleKeyPress}
          role="button"
          tabIndex={0}>
          {baseContent}
        </span>
        <Popover
          id="clickable-popover"
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
          onClose={this.handleClose}>
          {popoverContent}
        </Popover>
      </React.Fragment>
    );
  }
}

ClickablePopover.propTypes = {
  baseContent: PropTypes.node.isRequired,
  popoverContent: PropTypes.node.isRequired,
};

export default withStyles(styles)(ClickablePopover);
